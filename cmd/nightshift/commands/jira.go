package commands

import "github.com/spf13/cobra"

var jiraCmd = &cobra.Command{
	Use:   "jira",
	Short: "Jira autonomous backlog worker",
	Long: `Commands for the Jira-driven autonomous system.
Fetches tickets, validates, implements, creates PRs, and handles review feedback.`,
}

func init() {
	rootCmd.AddCommand(jiraCmd)
}
