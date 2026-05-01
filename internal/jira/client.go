package jira

import (
	"context"
	"fmt"
	"os"

	"strings"

	atlassianjira "github.com/ctreminiom/go-atlassian/v2/jira/v3"
	model "github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"
	"github.com/marcus/nightshift/internal/logging"
)

// Client wraps the go-atlassian Jira client with nightshift-specific helpers.
type Client struct {
	jira      *atlassianjira.Client
	cfg       JiraConfig
	log       *logging.Logger
	statusMap *StatusMap // cached result of DiscoverStatuses
}

// NewClient creates a Jira client from nightshift config.
// Reads API token from the env var specified in cfg.TokenEnv.
// cfg.Site is a subdomain (e.g. "mysite"); the full URL is constructed as
// https://<site>.atlassian.net.
func NewClient(cfg JiraConfig) (*Client, error) {
	if cfg.Site == "" {
		return nil, fmt.Errorf("jira: site is required")
	}
	if cfg.Email == "" {
		return nil, fmt.Errorf("jira: email is required")
	}
	if cfg.TokenEnv == "" {
		return nil, fmt.Errorf("jira: token_env is required")
	}
	apiToken := os.Getenv(cfg.TokenEnv)
	if apiToken == "" {
		return nil, fmt.Errorf("jira: env var %s not set", cfg.TokenEnv)
	}
	siteURL := fmt.Sprintf("https://%s.atlassian.net", cfg.Site)
	client, err := atlassianjira.New(nil, siteURL)
	if err != nil {
		return nil, fmt.Errorf("jira: creating client: %w", err)
	}
	client.Auth.SetBasicAuth(cfg.Email, apiToken)
	return &Client{
		jira: client,
		cfg:  cfg,
		log:  logging.Component("jira"),
	}, nil
}

// Ping validates the connection by fetching the current user.
func (c *Client) Ping(ctx context.Context) error {
	_, _, err := c.jira.MySelf.Details(ctx, nil)
	if err != nil {
		return fmt.Errorf("jira: ping failed: %w", err)
	}
	return nil
}

// Raw returns the underlying go-atlassian client for direct API access.
func (c *Client) Raw() *atlassianjira.Client { return c.jira }

// ProjectKey returns the configured Jira project key.
func (c *Client) ProjectKey() string { return c.cfg.Project }

// Label returns the configured ticket filter label.
func (c *Client) Label() string { return c.cfg.Label }

// AddComment posts a comment on the given Jira issue using ADF format.
// The body is split on blank lines into paragraphs; newlines within a paragraph
// become ADF hardBreak nodes so the text renders correctly in Jira.
func (c *Client) AddComment(ctx context.Context, issueKey, body string) error {
	var adfContent []*model.CommentNodeScheme
	for _, para := range strings.Split(body, "\n\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		lines := strings.Split(para, "\n")
		var nodes []*model.CommentNodeScheme
		for i, line := range lines {
			if i > 0 {
				nodes = append(nodes, &model.CommentNodeScheme{Type: "hardBreak"})
			}
			if line != "" {
				nodes = append(nodes, &model.CommentNodeScheme{Type: "text", Text: line})
			}
		}
		adfContent = append(adfContent, &model.CommentNodeScheme{
			Type:    "paragraph",
			Content: nodes,
		})
	}
	if len(adfContent) == 0 {
		adfContent = []*model.CommentNodeScheme{
			{Type: "paragraph", Content: []*model.CommentNodeScheme{{Type: "text", Text: body}}},
		}
	}
	payload := &model.CommentPayloadScheme{
		Body: &model.CommentNodeScheme{
			Version: 1,
			Type:    "doc",
			Content: adfContent,
		},
	}
	_, _, err := c.jira.Issue.Comment.Add(ctx, issueKey, payload, nil)
	if err != nil {
		return fmt.Errorf("jira: add comment to %s: %w", issueKey, err)
	}
	return nil
}
