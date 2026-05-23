// Package analysis provides code ownership and bus-factor analysis tools.
package analysis

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// CommitAuthor represents a commit author's contribution.
type CommitAuthor struct {
	Name    string
	Email   string
	Commits int
}

// GitRunner executes git commands in a repository directory.
type GitRunner interface {
	Run(repoPath string, args ...string) ([]byte, error)
}

// execGitRunner is the default GitRunner using os/exec.
type execGitRunner struct{}

func (execGitRunner) Run(repoPath string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoPath
	return cmd.Output()
}

// Option configures a GitParser.
type Option func(*GitParser)

// WithGitRunner overrides the GitRunner used by the parser.
func WithGitRunner(r GitRunner) Option {
	return func(gp *GitParser) { gp.runner = r }
}

// GitParser extracts commit history from a git repository.
type GitParser struct {
	repoPath string
	runner   GitRunner
}

// NewGitParser creates a new git parser for the given repository path.
func NewGitParser(repoPath string, opts ...Option) *GitParser {
	gp := &GitParser{repoPath: repoPath, runner: execGitRunner{}}
	for _, o := range opts {
		o(gp)
	}
	return gp
}

// ParseAuthors extracts authors and their commit counts from git history.
// Supports filtering by date range and file patterns.
func (gp *GitParser) ParseAuthors(opts ParseOptions) ([]CommitAuthor, error) {
	args := []string{"log", "--format=%an|%ae"}

	// Add date filtering if specified
	if !opts.Since.IsZero() {
		args = append(args, fmt.Sprintf("--since=%s", opts.Since.Format(time.RFC3339)))
	}
	if !opts.Until.IsZero() {
		args = append(args, fmt.Sprintf("--until=%s", opts.Until.Format(time.RFC3339)))
	}

	// Add file pattern filtering if specified
	if opts.FilePath != "" {
		args = append(args, "--", opts.FilePath)
	}

	output, err := gp.runner.Run(gp.repoPath, args...)
	if err != nil {
		return nil, fmt.Errorf("running git log: %w", err)
	}

	// Parse output and aggregate by author
	authorMap := make(map[string]*CommitAuthor)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) != 2 {
			continue
		}

		name := parts[0]
		email := parts[1]
		key := strings.ToLower(email) // Use email as canonical identifier

		if author, exists := authorMap[key]; exists {
			author.Commits++
		} else {
			authorMap[key] = &CommitAuthor{
				Name:    name,
				Email:   email,
				Commits: 1,
			}
		}
	}

	// Convert map to slice
	authors := make([]CommitAuthor, 0, len(authorMap))
	for _, author := range authorMap {
		authors = append(authors, *author)
	}

	return authors, nil
}

// ParseOptions defines filtering options for git history parsing.
type ParseOptions struct {
	Since    time.Time
	Until    time.Time
	FilePath string
}

// RepositoryExists checks if a valid git repository exists at the given path.
func RepositoryExists(path string) bool {
	gitDir := strings.TrimSpace(path) + "/.git"
	_, err := os.Stat(gitDir)
	return err == nil
}
