// Package snapshots collects and stores periodic usage data from AI providers.
package snapshots

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/marcus/nightshift/internal/db"
	"github.com/marcus/nightshift/internal/usage"
)

// AnthropicAPIClient fetches usage quotas from the Anthropic API.
type AnthropicAPIClient interface {
	FetchQuotas(ctx context.Context) (usage.AnthropicQuotaResponse, error)
}

// CodexAPIClient fetches usage data from the Codex/OpenAI API.
type CodexAPIClient interface {
	FetchUsage(ctx context.Context) (*usage.CodexUsageResponse, error)
}

// CopilotAPIClient fetches quota data from the GitHub Copilot API.
type CopilotAPIClient interface {
	FetchQuotas(ctx context.Context) (*usage.CopilotUserResponse, error)
}

// Snapshot represents a stored usage snapshot.
type Snapshot struct {
	ID               int64
	Provider         string
	Timestamp        time.Time
	WeekStart        time.Time
	LocalTokens      int64
	LocalDaily       int64
	ScrapedPct       *float64
	InferredBudget   *int64
	DayOfWeek        int
	HourOfDay        int
	WeekNumber       int
	Year             int
	SessionResetTime string
	WeeklyResetTime  string
	Source           string
	ScrapeErr        error `json:"-"`
}

// HourlyAverage represents average daily tokens by hour.
type HourlyAverage struct {
	Hour           int
	AvgDailyTokens float64
}

// Collector gathers and stores usage snapshots.
type Collector struct {
	db           *db.DB
	weekStartDay time.Weekday
	anthropicAPI AnthropicAPIClient
	codexAPI     CodexAPIClient
	copilotAPI   CopilotAPIClient
}

// NewCollector creates a snapshot collector. API clients are optional; when nil,
// TakeSnapshot will error for that provider. For history/query-only use, all clients may be nil.
func NewCollector(database *db.DB, weekStartDay time.Weekday) *Collector {
	return NewCollectorWithAPIs(database, weekStartDay, nil, nil, nil)
}

// NewCollectorWithAPIs creates a snapshot collector with API clients for active tracking.
func NewCollectorWithAPIs(
	database *db.DB,
	weekStartDay time.Weekday,
	anthropicAPI AnthropicAPIClient,
	codexAPI CodexAPIClient,
	copilotAPI CopilotAPIClient,
) *Collector {
	if weekStartDay < time.Sunday || weekStartDay > time.Saturday {
		weekStartDay = time.Monday
	}
	return &Collector{
		db:           database,
		weekStartDay: weekStartDay,
		anthropicAPI: anthropicAPI,
		codexAPI:     codexAPI,
		copilotAPI:   copilotAPI,
	}
}

// TakeSnapshot collects and stores a snapshot for the provider using active (API-based) tracking.
func (c *Collector) TakeSnapshot(ctx context.Context, provider string) (Snapshot, error) {
	if c == nil || c.db == nil {
		return Snapshot{}, errors.New("db is nil")
	}

	provider = strings.ToLower(provider)
	now := time.Now()

	var localWeekly, localDaily int64
	var scrapedPct *float64
	var sessionResetTime, weeklyResetTime string
	var source string
	var err error

	switch provider {
	case "claude":
		if c.anthropicAPI == nil {
			return Snapshot{}, errors.New("claude api client is nil")
		}
		localWeekly, localDaily, scrapedPct, sessionResetTime, weeklyResetTime, source, err = c.collectClaudeFromAPI(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("claude api: %w", err)
		}
	case "codex":
		if c.codexAPI == nil {
			return Snapshot{}, errors.New("codex api client is nil")
		}
		localWeekly, localDaily, scrapedPct, sessionResetTime, weeklyResetTime, source, err = c.collectCodexFromAPI(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("codex api: %w", err)
		}
	case "copilot":
		if c.copilotAPI == nil {
			return Snapshot{}, errors.New("copilot api client is nil")
		}
		localWeekly, localDaily, scrapedPct, sessionResetTime, weeklyResetTime, source, err = c.collectCopilotFromAPI(ctx)
		if err != nil {
			return Snapshot{}, fmt.Errorf("copilot api: %w", err)
		}
	default:
		return Snapshot{}, fmt.Errorf("unknown provider: %s", provider)
	}

	weekStart := startOfWeek(now, c.weekStartDay)
	dayOfWeek := int(now.Weekday())
	hourOfDay := now.Hour()
	weekNumber, year := weekStart.ISOWeek()

	var inferredBudget *int64
	if scrapedPct != nil && *scrapedPct > 0 && localWeekly > 0 {
		budget := int64(math.Round(float64(localWeekly) / (*scrapedPct / 100)))
		inferredBudget = &budget
	}

	result, err := c.db.SQL().Exec(
		`INSERT INTO snapshots (provider, timestamp, week_start, local_tokens, local_daily, scraped_pct, inferred_budget, day_of_week, hour_of_day, week_number, year, session_reset_time, weekly_reset_time, source)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		provider,
		now,
		weekStart,
		localWeekly,
		localDaily,
		nullFloat(scrapedPct),
		nullInt(inferredBudget),
		dayOfWeek,
		hourOfDay,
		weekNumber,
		year,
		nullString(sessionResetTime),
		nullString(weeklyResetTime),
		source,
	)
	if err != nil {
		return Snapshot{}, fmt.Errorf("insert snapshot: %w", err)
	}

	id, _ := result.LastInsertId()

	return Snapshot{
		ID:               id,
		Provider:         provider,
		Timestamp:        now,
		WeekStart:        weekStart,
		LocalTokens:      localWeekly,
		LocalDaily:       localDaily,
		ScrapedPct:       scrapedPct,
		InferredBudget:   inferredBudget,
		DayOfWeek:        dayOfWeek,
		HourOfDay:        hourOfDay,
		WeekNumber:       weekNumber,
		Year:             year,
		SessionResetTime: sessionResetTime,
		WeeklyResetTime:  weeklyResetTime,
		Source:           source,
	}, nil
}

// collectClaudeFromAPI fetches Claude usage from the Anthropic API.
func (c *Collector) collectClaudeFromAPI(ctx context.Context) (localWeekly, localDaily int64, scrapedPct *float64, sessionResetTime, weeklyResetTime, source string, err error) {
	resp, err := c.anthropicAPI.FetchQuotas(ctx)
	if err != nil {
		return 0, 0, nil, "", "", "", err
	}

	if entry, ok := resp["seven_day"]; ok && entry.IsEnabled {
		pct := clampPct(entry.Utilization * 100)
		scrapedPct = &pct
		if !entry.ResetsAt.IsZero() {
			weeklyResetTime = entry.ResetsAt.Format(time.RFC3339)
		} else if entry.ResetsAtRaw != "" {
			weeklyResetTime = entry.ResetsAtRaw
		}
	}
	if entry, ok := resp["five_hour"]; ok && entry.IsEnabled {
		if !entry.ResetsAt.IsZero() {
			sessionResetTime = entry.ResetsAt.Format(time.RFC3339)
		} else if entry.ResetsAtRaw != "" {
			sessionResetTime = entry.ResetsAtRaw
		}
	}

	// Active tracking doesn't use local token counts; these are always 0 and inferred_budget is not computed.
	return 0, 0, scrapedPct, sessionResetTime, weeklyResetTime, "api", nil
}

// collectCodexFromAPI fetches Codex usage from the OpenAI API.
func (c *Collector) collectCodexFromAPI(ctx context.Context) (localWeekly, localDaily int64, scrapedPct *float64, sessionResetTime, weeklyResetTime, source string, err error) {
	resp, err := c.codexAPI.FetchUsage(ctx)
	if err != nil {
		return 0, 0, nil, "", "", "", err
	}

	if resp.RateLimit != nil && resp.RateLimit.PrimaryWindow != nil {
		pw := resp.RateLimit.PrimaryWindow
		pct := clampPct(pw.UsedPercent)
		scrapedPct = &pct
		if !pw.ResetAt.IsZero() {
			sessionResetTime = pw.ResetAt.Format(time.RFC3339)
			weeklyResetTime = sessionResetTime
		}
	}

	// Active tracking doesn't use local token counts; these are always 0 and inferred_budget is not computed.
	return 0, 0, scrapedPct, sessionResetTime, weeklyResetTime, "api", nil
}

// collectCopilotFromAPI fetches Copilot usage from the GitHub API.
func (c *Collector) collectCopilotFromAPI(ctx context.Context) (localWeekly, localDaily int64, scrapedPct *float64, sessionResetTime, weeklyResetTime, source string, err error) {
	resp, err := c.copilotAPI.FetchQuotas(ctx)
	if err != nil {
		return 0, 0, nil, "", "", "", err
	}
	if resp == nil {
		return 0, 0, nil, "", "", "", errors.New("copilot api: nil response")
	}

	if quota, ok := resp.Quotas["premium_interactions"]; ok && !quota.Unlimited {
		pct := clampPct(100.0 - quota.PercentRemaining)
		scrapedPct = &pct
	}
	if resp.QuotaResetDate != "" {
		weeklyResetTime = resp.QuotaResetDate
		sessionResetTime = resp.QuotaResetDate
	}

	// Active tracking doesn't use local token counts; these are always 0 and inferred_budget is not computed.
	return 0, 0, scrapedPct, sessionResetTime, weeklyResetTime, "api", nil
}

// GetLatest returns the latest snapshots for a provider.
func (c *Collector) GetLatest(provider string, n int) ([]Snapshot, error) {
	if n <= 0 {
		return []Snapshot{}, nil
	}
	rows, err := c.db.SQL().Query(
		`SELECT id, provider, timestamp, week_start, local_tokens, local_daily, scraped_pct, inferred_budget, day_of_week, hour_of_day, week_number, year, session_reset_time, weekly_reset_time, source
		 FROM snapshots
		 WHERE provider = ?
		 ORDER BY timestamp DESC
		 LIMIT ?`,
		strings.ToLower(provider),
		n,
	)
	if err != nil {
		return nil, fmt.Errorf("query latest snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []Snapshot
	for rows.Next() {
		snapshot, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshots: %w", err)
	}
	return snapshots, nil
}

// GetSinceWeekStart returns snapshots from the current week.
func (c *Collector) GetSinceWeekStart(provider string) ([]Snapshot, error) {
	weekStart := startOfWeek(time.Now(), c.weekStartDay)
	rows, err := c.db.SQL().Query(
		`SELECT id, provider, timestamp, week_start, local_tokens, local_daily, scraped_pct, inferred_budget, day_of_week, hour_of_day, week_number, year, session_reset_time, weekly_reset_time, source
		 FROM snapshots
		 WHERE provider = ? AND week_start = ?
		 ORDER BY timestamp ASC`,
		strings.ToLower(provider),
		weekStart,
	)
	if err != nil {
		return nil, fmt.Errorf("query week snapshots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var snapshots []Snapshot
	for rows.Next() {
		snapshot, err := scanSnapshot(rows)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate snapshots: %w", err)
	}
	return snapshots, nil
}

// GetHourlyAverages returns average daily tokens per hour.
func (c *Collector) GetHourlyAverages(provider string, lookbackDays int) ([]HourlyAverage, error) {
	if lookbackDays <= 0 {
		return []HourlyAverage{}, nil
	}
	cutoff := time.Now().AddDate(0, 0, -lookbackDays)
	rows, err := c.db.SQL().Query(
		`SELECT hour_of_day, AVG(local_daily)
		 FROM snapshots
		 WHERE provider = ? AND timestamp >= ?
		 GROUP BY hour_of_day
		 ORDER BY hour_of_day ASC`,
		strings.ToLower(provider),
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("query hourly averages: %w", err)
	}
	defer func() { _ = rows.Close() }()

	averages := make([]HourlyAverage, 0)
	for rows.Next() {
		var avg HourlyAverage
		if err := rows.Scan(&avg.Hour, &avg.AvgDailyTokens); err != nil {
			return nil, fmt.Errorf("scan hourly average: %w", err)
		}
		averages = append(averages, avg)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate hourly averages: %w", err)
	}
	return averages, nil
}

// Prune deletes snapshots older than retentionDays.
func (c *Collector) Prune(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	result, err := c.db.SQL().Exec(`DELETE FROM snapshots WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("prune snapshots: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune snapshots: %w", err)
	}
	return deleted, nil
}

func scanSnapshot(rows *sql.Rows) (Snapshot, error) {
	var snapshot Snapshot
	var scraped sql.NullFloat64
	var inferred sql.NullInt64
	var sessionReset, weeklyReset sql.NullString
	var source sql.NullString
	if err := rows.Scan(
		&snapshot.ID,
		&snapshot.Provider,
		&snapshot.Timestamp,
		&snapshot.WeekStart,
		&snapshot.LocalTokens,
		&snapshot.LocalDaily,
		&scraped,
		&inferred,
		&snapshot.DayOfWeek,
		&snapshot.HourOfDay,
		&snapshot.WeekNumber,
		&snapshot.Year,
		&sessionReset,
		&weeklyReset,
		&source,
	); err != nil {
		return Snapshot{}, fmt.Errorf("scan snapshot: %w", err)
	}
	if scraped.Valid {
		snapshot.ScrapedPct = &scraped.Float64
	}
	if inferred.Valid {
		value := inferred.Int64
		snapshot.InferredBudget = &value
	}
	if sessionReset.Valid {
		snapshot.SessionResetTime = sessionReset.String
	}
	if weeklyReset.Valid {
		snapshot.WeeklyResetTime = weeklyReset.String
	}
	if source.Valid {
		snapshot.Source = source.String
	} else {
		// Legacy snapshots created before source column existed default to "unknown"
		snapshot.Source = "unknown"
	}
	return snapshot, nil
}

func startOfWeek(now time.Time, weekStartDay time.Weekday) time.Time {
	if weekStartDay < time.Sunday || weekStartDay > time.Saturday {
		weekStartDay = time.Monday
	}

	now = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	delta := (7 + int(now.Weekday()) - int(weekStartDay)) % 7
	return now.AddDate(0, 0, -delta)
}

func nullFloat(value *float64) any {
	if value == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *value, Valid: true}
}

func nullInt(value *int64) any {
	if value == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *value, Valid: true}
}

func nullString(value string) any {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

func clampPct(pct float64) float64 {
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}
