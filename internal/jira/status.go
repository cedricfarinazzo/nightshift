package jira

import (
	"context"
	"fmt"
	"strings"

	model "github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"
)

// Status represents a Jira status with its transition info.
type Status struct {
	ID          string // Jira status ID
	Name        string // Display name (e.g., "À faire", "In Progress")
	CategoryKey string // "new", "indeterminate", "done"
}

// StatusMap holds the discovered status-to-transition mapping for a project.
type StatusMap struct {
	TodoStatuses       []Status // statuses in "new" category
	InProgressStatuses []Status // statuses in "indeterminate" (non-review)
	ReviewStatuses     []Status // statuses in "indeterminate" matching review heuristic
	DoneStatuses       []Status // statuses in "done" category
	NeedsInfoStatus    *Status  // status for rejected tickets (nil if not found)
}

var reviewKeywords = []string{"review", "revue", "qa", "testing", "validation"}

func isReviewStatus(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range reviewKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

var needsInfoKeywords = []string{"needs info", "needs-info", "more info", "need info"}

func isNeedsInfoStatus(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range needsInfoKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// buildStatusMap classifies a flat list of project statuses into a StatusMap.
func buildStatusMap(statuses []*model.ProjectStatusDetailsScheme) *StatusMap {
	sm := &StatusMap{}
	for _, s := range statuses {
		if s == nil || s.StatusCategory == nil {
			continue
		}
		st := Status{
			ID:          s.ID,
			Name:        s.Name,
			CategoryKey: s.StatusCategory.Key,
		}
		switch s.StatusCategory.Key {
		case "new":
			if isNeedsInfoStatus(s.Name) && sm.NeedsInfoStatus == nil {
				cp := st
				sm.NeedsInfoStatus = &cp
			}
			sm.TodoStatuses = append(sm.TodoStatuses, st)
		case "indeterminate":
			if isNeedsInfoStatus(s.Name) && sm.NeedsInfoStatus == nil {
				cp := st
				sm.NeedsInfoStatus = &cp
			}
			if isReviewStatus(s.Name) {
				sm.ReviewStatuses = append(sm.ReviewStatuses, st)
			} else {
				sm.InProgressStatuses = append(sm.InProgressStatuses, st)
			}
		case "done":
			sm.DoneStatuses = append(sm.DoneStatuses, st)
		}
	}
	return sm
}

// DiscoverStatuses fetches all statuses for the configured project and classifies them.
func (c *Client) DiscoverStatuses(ctx context.Context) (*StatusMap, error) {
	pages, _, err := c.jira.Project.Statuses(ctx, c.cfg.Project)
	if err != nil {
		return nil, fmt.Errorf("jira: discovering statuses: %w", err)
	}
	var all []*model.ProjectStatusDetailsScheme
	for _, page := range pages {
		all = append(all, page.Statuses...)
	}
	return buildStatusMap(all), nil
}

// FindTransition finds a valid transition ID to a status in the target category.
func (c *Client) FindTransition(ctx context.Context, issueKey, targetCategoryKey string) (string, error) {
	result, _, err := c.jira.Issue.Transitions(ctx, issueKey)
	if err != nil {
		return "", fmt.Errorf("jira: getting transitions for %s: %w", issueKey, err)
	}
	for _, t := range result.Transitions {
		if t.To != nil && t.To.StatusCategory != nil && t.To.StatusCategory.Key == targetCategoryKey {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("jira: no transition to category %q available for %s", targetCategoryKey, issueKey)
}

// findTransitionToStatus finds a transition ID that moves the issue to a specific status ID.
func (c *Client) findTransitionToStatus(ctx context.Context, issueKey, statusID string) (string, error) {
	result, _, err := c.jira.Issue.Transitions(ctx, issueKey)
	if err != nil {
		return "", fmt.Errorf("jira: getting transitions for %s: %w", issueKey, err)
	}
	for _, t := range result.Transitions {
		if t.To != nil && t.To.ID == statusID {
			return t.ID, nil
		}
	}
	return "", fmt.Errorf("jira: no transition to status ID %q available for %s", statusID, issueKey)
}

// TransitionTo moves an issue to the first available status in the target category.
func (c *Client) TransitionTo(ctx context.Context, issueKey, targetCategoryKey string) error {
	transitionID, err := c.FindTransition(ctx, issueKey, targetCategoryKey)
	if err != nil {
		return err
	}
	_, err = c.jira.Issue.Move(ctx, issueKey, transitionID, nil)
	if err != nil {
		return fmt.Errorf("jira: transitioning %s to category %q: %w", issueKey, targetCategoryKey, err)
	}
	return nil
}

// TransitionToInProgress moves the ticket to the first non-review "indeterminate" status.
func (c *Client) TransitionToInProgress(ctx context.Context, issueKey string) error {
	sm, err := c.DiscoverStatuses(ctx)
	if err != nil {
		return err
	}
	if len(sm.InProgressStatuses) == 0 {
		return fmt.Errorf("jira: no in-progress status found in project %s", c.cfg.Project)
	}
	transitionID, err := c.findTransitionToStatus(ctx, issueKey, sm.InProgressStatuses[0].ID)
	if err != nil {
		return err
	}
	_, err = c.jira.Issue.Move(ctx, issueKey, transitionID, nil)
	if err != nil {
		return fmt.Errorf("jira: transitioning %s to in-progress: %w", issueKey, err)
	}
	return nil
}

// TransitionToReview moves the ticket to the review status.
func (c *Client) TransitionToReview(ctx context.Context, issueKey string) error {
	sm, err := c.DiscoverStatuses(ctx)
	if err != nil {
		return err
	}
	if len(sm.ReviewStatuses) == 0 {
		return fmt.Errorf("jira: no review status found in project %s", c.cfg.Project)
	}
	transitionID, err := c.findTransitionToStatus(ctx, issueKey, sm.ReviewStatuses[0].ID)
	if err != nil {
		return err
	}
	_, err = c.jira.Issue.Move(ctx, issueKey, transitionID, nil)
	if err != nil {
		return fmt.Errorf("jira: transitioning %s to review: %w", issueKey, err)
	}
	return nil
}

// TransitionToDone moves the ticket to the first "done" status.
func (c *Client) TransitionToDone(ctx context.Context, issueKey string) error {
	return c.TransitionTo(ctx, issueKey, "done")
}

// TransitionToNeedsInfo moves the ticket to a needs-info status if one exists,
// otherwise falls back to the first "new" (todo) status.
func (c *Client) TransitionToNeedsInfo(ctx context.Context, issueKey string) error {
	sm, err := c.DiscoverStatuses(ctx)
	if err != nil {
		return err
	}
	if sm.NeedsInfoStatus != nil {
		transitionID, err := c.findTransitionToStatus(ctx, issueKey, sm.NeedsInfoStatus.ID)
		if err != nil {
			return err
		}
		_, err = c.jira.Issue.Move(ctx, issueKey, transitionID, nil)
		if err != nil {
			return fmt.Errorf("jira: transitioning %s to needs-info: %w", issueKey, err)
		}
		return nil
	}
	// Fallback: transition to todo (new category)
	return c.TransitionTo(ctx, issueKey, "new")
}
