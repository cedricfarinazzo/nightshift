package commands

import (
	"fmt"
	"strings"
	"time"

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
func formatQuotaWindowLabel(window string) string {
	switch {
	case window == "five_hour" || strings.HasSuffix(window, "s") && strings.Contains(window, "18000"):
		return "5-Hour"
	case window == "seven_day" || window == "weekly":
		return "Weekly"
	case window == "monthly" || window == "one_month":
		return "Monthly"
	case window == "premium_interactions":
		return "Premium"
	case strings.HasSuffix(window, "-primary"):
		return "5h-pri"
	default:
		if len(window) > 8 {
			return window[:8]
		}
		return window
	}
}
