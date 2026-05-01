package jira

import (
	"context"
	"fmt"
	"strings"

	model "github.com/ctreminiom/go-atlassian/v2/pkg/infra/models"
)

// Status represents a Jira status with its ID, display name, and category.
type Status struct {
	ID          string // Jira status ID
	Name        string // Display name (e.g., "À faire", "In Progress")
	CategoryKey string // "new", "indeterminate", "done"
}

// StatusMap holds the discovered statuses for a project, grouped by category and role.
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

// DiscoverStatuses fetches and classifies statuses across all configured projects.
// The result is cached on the client for the lifetime of the client instance.
func (c *Client) DiscoverStatuses(ctx context.Context) (*StatusMap, error) {
	if c.statusMap != nil {
		return c.statusMap, nil
	}

	// Collect all project keys: prefer Projects list, fall back to deprecated flat field.
	keys := make([]string, 0, len(c.cfg.Projects)+1)
	for _, p := range c.cfg.Projects {
		if p.Key != "" {
			keys = append(keys, p.Key)
		}
	}
	if len(keys) == 0 && c.cfg.Project != "" {
		keys = append(keys, c.cfg.Project)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("jira: no project id or key set")
	}

	var all []*model.ProjectStatusDetailsScheme
	for _, key := range keys {
		pages, _, err := c.jira.Project.Statuses(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("jira: discovering statuses for %s: %w", key, err)
		}
		for _, page := range pages {
			all = append(all, page.Statuses...)
		}
	}
	c.statusMap = buildStatusMap(all)
	return c.statusMap, nil
}

// getTransitions fetches available transitions for an issue, indexed by destination status ID.
func (c *Client) getTransitions(ctx context.Context, issueKey string) (map[string]string, error) {
	result, _, err := c.jira.Issue.Transitions(ctx, issueKey)
	if err != nil {
		return nil, fmt.Errorf("jira: getting transitions for %s: %w", issueKey, err)
	}
	byStatusID := make(map[string]string, len(result.Transitions))
	for _, t := range result.Transitions {
		if t.To != nil {
			byStatusID[t.To.ID] = t.ID
		}
	}
	return byStatusID, nil
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

// firstReachable returns the transition ID for the first status in candidates that has an
// available transition, using the pre-fetched byStatusID map (status ID → transition ID).
func firstReachable(candidates []Status, byStatusID map[string]string) (string, bool) {
	for _, s := range candidates {
		if tid, ok := byStatusID[s.ID]; ok {
			return tid, true
		}
	}
	return "", false
}

// TransitionToInProgress moves the ticket to the first reachable non-review "indeterminate" status.
func (c *Client) TransitionToInProgress(ctx context.Context, issueKey string) error {
	sm, err := c.DiscoverStatuses(ctx)
	if err != nil {
		return err
	}
	if len(sm.InProgressStatuses) == 0 {
		return fmt.Errorf("jira: no in-progress status found in project %s", c.cfg.Project)
	}
	byStatusID, err := c.getTransitions(ctx, issueKey)
	if err != nil {
		return err
	}
	transitionID, ok := firstReachable(sm.InProgressStatuses, byStatusID)
	if !ok {
		return fmt.Errorf("jira: no reachable in-progress transition available for %s", issueKey)
	}
	_, err = c.jira.Issue.Move(ctx, issueKey, transitionID, nil)
	if err != nil {
		return fmt.Errorf("jira: transitioning %s to in-progress: %w", issueKey, err)
	}
	return nil
}

// TransitionToReview moves the ticket to the first reachable review status.
func (c *Client) TransitionToReview(ctx context.Context, issueKey string) error {
	sm, err := c.DiscoverStatuses(ctx)
	if err != nil {
		return err
	}
	if len(sm.ReviewStatuses) == 0 {
		return fmt.Errorf("jira: no review status found in project %s", c.cfg.Project)
	}
	byStatusID, err := c.getTransitions(ctx, issueKey)
	if err != nil {
		return err
	}
	transitionID, ok := firstReachable(sm.ReviewStatuses, byStatusID)
	if !ok {
		return fmt.Errorf("jira: no reachable review transition available for %s", issueKey)
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

// TransitionToNeedsInfo moves the ticket to a needs-info status if one exists and is reachable,
// otherwise falls back to the first "new" (todo) status.
func (c *Client) TransitionToNeedsInfo(ctx context.Context, issueKey string) error {
	sm, err := c.DiscoverStatuses(ctx)
	if err != nil {
		return err
	}
	if sm.NeedsInfoStatus != nil {
		byStatusID, err := c.getTransitions(ctx, issueKey)
		if err != nil {
			return err
		}
		if transitionID, ok := byStatusID[sm.NeedsInfoStatus.ID]; ok {
			_, err = c.jira.Issue.Move(ctx, issueKey, transitionID, nil)
			if err != nil {
				return fmt.Errorf("jira: transitioning %s to needs-info: %w", issueKey, err)
			}
			return nil
		}
		// needs-info status exists but is not reachable; fall through to todo
	}
	return c.TransitionTo(ctx, issueKey, "new")
}
