package commands

import (
	"fmt"
	"strings"

	"github.com/cedricfarinazzo/nightshift/internal/jira"
	"github.com/spf13/cobra"
)

var jiraCmd = &cobra.Command{
	Use:   "jira",
	Short: "Jira autonomous backlog worker",
	Long: `Commands for the Jira-driven autonomous system.
Fetches tickets, validates, implements, creates PRs, and handles review feedback.`,
}

func init() {
	rootCmd.AddCommand(jiraCmd)
}

// addProjectFlag registers the --project/-p flag on cmd with the canonical
// help text. Call from init() in both jira_run.go and jira_preview.go so the
// paired commands stay in sync.
func addProjectFlag(cmd *cobra.Command) {
	cmd.Flags().StringP("project", "p", "", "Jira project key — process only this project (default: all configured)")
}

// filterProjectsByKey returns only the projects whose Key matches key
// (case-insensitive). Empty key returns projects unchanged. No match returns
// an error listing configured keys.
func filterProjectsByKey(projects []jira.ProjectConfig, key string) ([]jira.ProjectConfig, error) {
	if key == "" {
		return projects, nil
	}
	if len(projects) == 0 {
		return nil, fmt.Errorf("no Jira projects configured")
	}
	var filtered []jira.ProjectConfig
	for _, p := range projects {
		if strings.EqualFold(p.Key, key) {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		keys := make([]string, len(projects))
		for i, p := range projects {
			keys[i] = p.Key
		}
		return nil, fmt.Errorf("project %q not found in config (configured: %s)", key, strings.Join(keys, ", "))
	}
	return filtered, nil
}
