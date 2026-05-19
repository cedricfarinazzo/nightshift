package tasks

import (
	"testing"

	"github.com/cedricfarinazzo/nightshift/internal/config"
)

func TestRegisterCustomTasksFromConfig_Valid(t *testing.T) {
	t.Cleanup(func() { ClearCustom() })

	customs := []config.CustomTaskConfig{
		{
			Type:        "my-review",
			Name:        "My Code Review",
			Description: "Custom code review task",
			Category:    "pr",
			CostTier:    "high",
			RiskLevel:   "medium",
			Interval:    "48h",
		},
		{
			Type:        "my-scan",
			Name:        "My Security Scan",
			Description: "Custom security scanning",
			Category:    "analysis",
			CostTier:    "low",
			RiskLevel:   "low",
			Interval:    "24h",
		},
	}

	if err := RegisterCustomTasksFromConfig(customs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify both appear in registry
	def1, err := GetDefinition("my-review")
	if err != nil {
		t.Fatalf("GetDefinition(my-review) error: %v", err)
	}
	if def1.Name != "My Code Review" {
		t.Errorf("Name = %q, want %q", def1.Name, "My Code Review")
	}
	if def1.Category != CategoryPR {
		t.Errorf("Category = %d, want %d", def1.Category, CategoryPR)
	}
	if def1.CostTier != CostHigh {
		t.Errorf("CostTier = %d, want %d", def1.CostTier, CostHigh)
	}
	if def1.RiskLevel != RiskMedium {
		t.Errorf("RiskLevel = %d, want %d", def1.RiskLevel, RiskMedium)
	}

	def2, err := GetDefinition("my-scan")
	if err != nil {
		t.Fatalf("GetDefinition(my-scan) error: %v", err)
	}
	if def2.Name != "My Security Scan" {
		t.Errorf("Name = %q, want %q", def2.Name, "My Security Scan")
	}

	// Verify IsCustom returns true for both
	if !IsCustom("my-review") {
		t.Error("IsCustom(my-review) should be true")
	}
	if !IsCustom("my-scan") {
		t.Error("IsCustom(my-scan) should be true")
	}
}

func TestRegisterCustomTasksFromConfig_Defaults(t *testing.T) {
	t.Cleanup(func() { ClearCustom() })

	customs := []config.CustomTaskConfig{
		{
			Type:        "minimal-task",
			Name:        "Minimal Task",
			Description: "Only required fields",
		},
	}

	if err := RegisterCustomTasksFromConfig(customs); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	def, err := GetDefinition("minimal-task")
	if err != nil {
		t.Fatalf("GetDefinition(minimal-task) error: %v", err)
	}
	if def.Category != CategoryAnalysis {
		t.Errorf("Category = %d, want %d (CategoryAnalysis)", def.Category, CategoryAnalysis)
	}
	if def.CostTier != CostMedium {
		t.Errorf("CostTier = %d, want %d (CostMedium)", def.CostTier, CostMedium)
	}
	if def.RiskLevel != RiskLow {
		t.Errorf("RiskLevel = %d, want %d (RiskLow)", def.RiskLevel, RiskLow)
	}
	wantInterval := DefaultIntervalForCategory(CategoryAnalysis)
	if def.DefaultInterval != wantInterval {
		t.Errorf("DefaultInterval = %v, want %v", def.DefaultInterval, wantInterval)
	}
}

func TestRegisterCustomTasksFromConfig_BuiltInCollision(t *testing.T) {
	t.Cleanup(func() { ClearCustom() })

	customs := []config.CustomTaskConfig{
		{
			Type:        "lint-fix",
			Name:        "Colliding Task",
			Description: "Should fail because lint-fix is built-in",
		},
	}

	err := RegisterCustomTasksFromConfig(customs)
	if err == nil {
		t.Fatal("expected error for built-in collision, got nil")
	}
}

func TestRegisterCustomTasksFromConfig_Rollback(t *testing.T) {
	t.Cleanup(func() { ClearCustom() })

	customs := []config.CustomTaskConfig{
		{
			Type:        "good-task",
			Name:        "Good Task",
			Description: "This one should register then be rolled back",
			Category:    "pr",
		},
		{
			Type:        "lint-fix",
			Name:        "Colliding Task",
			Description: "Collides with built-in, triggers rollback",
		},
	}

	err := RegisterCustomTasksFromConfig(customs)
	if err == nil {
		t.Fatal("expected error for built-in collision, got nil")
	}

	// Verify good-task was rolled back
	if _, err := GetDefinition("good-task"); err == nil {
		t.Error("good-task should have been rolled back from registry")
	}
	if IsCustom("good-task") {
		t.Error("good-task should not be marked as custom after rollback")
	}
}

func TestParseCategoryString(t *testing.T) {
	tests := []struct {
		input string
		want  TaskCategory
	}{
		{"pr", CategoryPR},
		{"PR", CategoryPR},
		{" pr ", CategoryPR},
		{"analysis", CategoryAnalysis},
		{"Analysis", CategoryAnalysis},
		{"options", CategoryOptions},
		{"safe", CategorySafe},
		{"map", CategoryMap},
		{"emergency", CategoryEmergency},
		{"", CategoryAnalysis},
		{"unknown", CategoryAnalysis},
		{"  ", CategoryAnalysis},
	}
	for _, tt := range tests {
		got := parseCategoryString(tt.input)
		if got != tt.want {
			t.Errorf("parseCategoryString(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseCostTierString(t *testing.T) {
	tests := []struct {
		input string
		want  CostTier
	}{
		{"low", CostLow},
		{"Low", CostLow},
		{" low ", CostLow},
		{"medium", CostMedium},
		{"Medium", CostMedium},
		{"high", CostHigh},
		{"very-high", CostVeryHigh},
		{"Very-High", CostVeryHigh},
		{"", CostMedium},
		{"unknown", CostMedium},
		{"  ", CostMedium},
	}
	for _, tt := range tests {
		got := parseCostTierString(tt.input)
		if got != tt.want {
			t.Errorf("parseCostTierString(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestParseRiskLevelString(t *testing.T) {
	tests := []struct {
		input string
		want  RiskLevel
	}{
		{"low", RiskLow},
		{"Low", RiskLow},
		{" low ", RiskLow},
		{"medium", RiskMedium},
		{"Medium", RiskMedium},
		{"high", RiskHigh},
		{"High", RiskHigh},
		{"", RiskLow},
		{"unknown", RiskLow},
		{"  ", RiskLow},
	}
	for _, tt := range tests {
		got := parseRiskLevelString(tt.input)
		if got != tt.want {
			t.Errorf("parseRiskLevelString(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
