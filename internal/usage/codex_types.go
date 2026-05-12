package usage

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var (
	ErrNoCredentials = errors.New("codex: no credentials found")
	ErrTokenExpired  = errors.New("codex: token expired and refresh failed")
	ErrUsageNotFound = errors.New("codex: usage endpoint not found")
)

// CodexUsageResponse is the top-level API response from the usage endpoint.
type CodexUsageResponse struct {
	PlanType            string             `json:"plan_type"`
	RateLimit           *CodexAPIRateLimit `json:"rate_limit"`
	CodeReviewRateLimit *CodexAPIRateLimit `json:"code_review_rate_limit"`
	Credits             *CodexCredits      `json:"credits,omitempty"`
}

// CodexAPIRateLimit holds primary and secondary rate limit windows.
type CodexAPIRateLimit struct {
	PrimaryWindow   *CodexWindow `json:"primary_window"`
	SecondaryWindow *CodexWindow `json:"secondary_window"`
}

// CodexWindow represents a single rate limit window.
type CodexWindow struct {
	UsedPercent        float64   `json:"used_percent"`
	ResetAt            UnixTime  `json:"reset_at"`
	LimitWindowSeconds int64     `json:"limit_window_seconds"`
}

// UnixTime unmarshals a Unix timestamp integer as time.Time.
type UnixTime struct{ time.Time }

func (u *UnixTime) UnmarshalJSON(data []byte) error {
	var ts int64
	if err := json.Unmarshal(data, &ts); err != nil {
		return fmt.Errorf("codex reset_at: %w", err)
	}
	u.Time = time.Unix(ts, 0)
	return nil
}

// CodexCredits holds the credits balance, which the API may encode as float or string.
type CodexCredits struct {
	Balance float64
}

func (c *CodexCredits) UnmarshalJSON(data []byte) error {
	// Try object with balance field first
	var raw struct {
		Balance json.RawMessage `json:"balance"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("codex credits: %w", err)
	}
	if raw.Balance == nil {
		return nil
	}

	// Try numeric
	var f float64
	if err := json.Unmarshal(raw.Balance, &f); err == nil {
		c.Balance = f
		return nil
	}

	// Try string-encoded float
	var s string
	if err := json.Unmarshal(raw.Balance, &s); err == nil {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return fmt.Errorf("codex credits balance string: %w", err)
		}
		c.Balance = f
		return nil
	}

	return fmt.Errorf("codex credits: cannot parse balance from %s", string(raw.Balance))
}
