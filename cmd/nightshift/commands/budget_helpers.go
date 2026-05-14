package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/marcus/nightshift/internal/budget"
	"github.com/marcus/nightshift/internal/config"
)

func resolveProviderList(cfg *config.Config, filter string) ([]string, error) {
	filter = strings.ToLower(strings.TrimSpace(filter))
	if filter != "" {
		switch filter {
		case "claude":
			if !cfg.Providers.Claude.Enabled {
				return nil, fmt.Errorf("claude provider not enabled")
			}
		case "codex":
			if !cfg.Providers.Codex.Enabled {
				return nil, fmt.Errorf("codex provider not enabled")
			}
		case "copilot":
			if !cfg.Providers.Copilot.Enabled {
				return nil, fmt.Errorf("copilot provider not enabled")
			}
		default:
			return nil, fmt.Errorf("unknown provider: %s (valid: claude, codex, copilot)", filter)
		}
		return []string{filter}, nil
	}

	providerList := []string{}
	if cfg.Providers.Claude.Enabled {
		providerList = append(providerList, "claude")
	}
	if cfg.Providers.Codex.Enabled {
		providerList = append(providerList, "codex")
	}
	if cfg.Providers.Copilot.Enabled {
		providerList = append(providerList, "copilot")
	}

	return providerList, nil
}

// unicodeProgressBar returns a unicode block bar for the given percent (0–100) and width.
func unicodeProgressBar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	fill := percent
	if fill > 100 {
		fill = 100
	}
	filled := int(fill * float64(width) / 100)
	empty := width - filled
	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}

// formatResetCountdown returns a human-readable countdown to a reset time.
// Returns "now" if the time is in the past.
func formatResetCountdown(t time.Time) string {
	d := time.Until(t)
	if d <= 0 {
		return "now"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh%dm", h, m)
	}
	days := int(d.Hours() / 24)
	h := int(d.Hours()) % 24
	if h == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd %dh", days, h)
}

// formatQuotaWindowLabel maps API window key names to short display labels.
// Returns empty string for unknown windows so callers can skip them.
func formatQuotaWindowLabel(window string) string {
	switch {
	case window == "five_hour" || (strings.HasSuffix(window, "s") && strings.Contains(window, "18000")):
		return "5-Hour"
	case window == "seven_day" || window == "weekly":
		return "Weekly"
	case window == "monthly" || window == "one_month" || window == "monthly_limit":
		return "Monthly"
	case window == "extra_usage":
		return "Extra"
	case window == "premium_interactions":
		return "Premium"
	case window == "completions":
		return "Complete"
	case window == "chat":
		return "Chat"
	case strings.HasSuffix(window, "s-primary"):
		return primaryWindowLabel(strings.TrimSuffix(window, "s-primary"))
	case strings.HasSuffix(window, "s") && !strings.Contains(window, "_"):
		// Numeric second-based windows like "18000s"
		return secondsWindowLabel(strings.TrimSuffix(window, "s"))
	default:
		return ""
	}
}

func primaryWindowLabel(secStr string) string {
	secs, err := strconv.Atoi(secStr)
	if err != nil {
		return "pri"
	}
	hours := secs / 3600
	switch {
	case hours <= 5:
		return "5h-pri"
	case hours <= 24:
		return "1d-pri"
	default:
		return "7d-pri"
	}
}

// windowDisplayName maps raw API window keys to short human-readable names.
func windowDisplayName(key string) string {
	switch key {
	case "five_hour":
		return "5-hour"
	case "seven_day":
		return "weekly"
	case "monthly_limit":
		return "monthly"
	case "premium_interactions":
		return "premium"
	case "primary":
		return "weekly (primary)"
	case "secondary":
		return "secondary"
	default:
		return key
	}
}

// printHourlyCapacity prints a capacity line plus per-window input data.
func printHourlyCapacity(hcr budget.HourlyCapacityResult) {
	capBar := unicodeProgressBar(hcr.Capacity*100, 25)
	fmt.Printf("  Capacity %s %3.0f%%\n", capBar, hcr.Capacity*100)
	for _, w := range hcr.Windows {
		marker := " "
		if w.Name == hcr.BottleneckWindow {
			marker = "▶"
		}
		resetStr := ""
		if w.ResetIn > 0 {
			resetStr = "  resets in " + formatDuration(w.ResetIn)
		}
		fmt.Printf("    %s %-18s  used=%3.0f%%  cap=%3.0f%%%s\n",
			marker, windowDisplayName(w.Name), w.UsedPct, w.Capacity*100, resetStr)
	}
}

func secondsWindowLabel(secStr string) string {
	secs, err := strconv.Atoi(secStr)
	if err != nil {
		return secStr
	}
	hours := secs / 3600
	switch {
	case hours <= 1:
		return fmt.Sprintf("%dm", secs/60)
	case hours <= 5:
		return "5-Hour"
	case hours <= 24:
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dd", hours/24)
	}
}
