// Package jira provides a thin wrapper around the go-atlassian Jira client
// for use in the nightshift Jira integration.
package jira

import (
	"context"
	"fmt"
	"os"

	jiraV3 "github.com/ctreminiom/go-atlassian/v2/jira/v3"
	"github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"
	"github.com/marcus/nightshift/internal/config"
)

// Client wraps go-atlassian's Jira v3 client.
type Client struct {
	inner *jiraV3.Client
	cfg   *config.JiraConfig
}

// New creates a Jira client from configuration.
// The API token is read from the env var named by cfg.APITokenEnv.
func New(cfg *config.JiraConfig) (*Client, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("jira.url is not configured")
	}

	token := os.Getenv(cfg.APITokenEnv)
	if token == "" {
		return nil, fmt.Errorf("jira API token env var %q is not set", cfg.APITokenEnv)
	}

	inner, err := jiraV3.New(nil, cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("creating jira client: %w", err)
	}
	inner.Auth.SetBasicAuth(cfg.Email, token)

	return &Client{inner: inner, cfg: cfg}, nil
}

// CheckConnectivity verifies that the Jira instance is reachable and the
// credentials are valid by fetching server info.
func (c *Client) CheckConnectivity(ctx context.Context) error {
	_, _, err := c.inner.Server.Info(ctx)
	if err != nil {
		return fmt.Errorf("jira connectivity check failed: %w", err)
	}
	return nil
}

// FetchProjects retrieves details for the project keys listed in config.
// If no project keys are configured, it returns all accessible projects.
func (c *Client) FetchProjects(ctx context.Context) ([]*models.ProjectScheme, error) {
	if len(c.cfg.ProjectKeys) > 0 {
		return c.fetchByKeys(ctx, c.cfg.ProjectKeys)
	}
	return c.fetchAll(ctx)
}

func (c *Client) fetchByKeys(ctx context.Context, keys []string) ([]*models.ProjectScheme, error) {
	projects := make([]*models.ProjectScheme, 0, len(keys))
	for _, key := range keys {
		proj, _, err := c.inner.Project.Get(ctx, key, nil)
		if err != nil {
			return nil, fmt.Errorf("fetching jira project %q: %w", key, err)
		}
		projects = append(projects, proj)
	}
	return projects, nil
}

func (c *Client) fetchAll(ctx context.Context) ([]*models.ProjectScheme, error) {
	opts := &models.ProjectSearchOptionsScheme{}
	result, _, err := c.inner.Project.Search(ctx, opts, 0, 50)
	if err != nil {
		return nil, fmt.Errorf("fetching jira projects: %w", err)
	}
	return result.Values, nil
}
