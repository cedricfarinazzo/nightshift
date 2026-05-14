package providers

import (
	"os"
	"testing"
	"time"
)

func TestNewCopilot_Defaults(t *testing.T) {
	provider := NewCopilot()
	if provider.Name() != "copilot" {
		t.Errorf("Name() = %q, want %q", provider.Name(), "copilot")
	}
	if provider.dataPath == "" {
		t.Error("expected non-empty dataPath")
	}
}

func TestNewCopilotWithPath(t *testing.T) {
	customPath := "/custom/path"
	provider := NewCopilotWithPath(customPath)
	if provider.dataPath != customPath {
		t.Errorf("dataPath = %q, want %q", provider.dataPath, customPath)
	}
}

func TestCopilot_Cost(t *testing.T) {
	provider := NewCopilot()
	input, output := provider.Cost()
	if input != 0 || output != 0 {
		t.Errorf("Cost() = (%d, %d), want (0, 0) for request-based pricing", input, output)
	}
}

func TestCopilot_LoadUsageData_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewCopilotWithPath(tmpDir)

	data, err := provider.LoadUsageData()
	if err != nil {
		t.Fatalf("LoadUsageData() error: %v", err)
	}
	if data == nil {
		t.Fatal("expected non-nil data for missing file")
	} else if data.RequestCount != 0 {
		t.Errorf("RequestCount = %d, want 0", data.RequestCount)
	}

	// Should have current month
	now := time.Now().UTC()
	expectedMonth := now.Format("2006-01")
	if data.Month != expectedMonth {
		t.Errorf("Month = %q, want %q", data.Month, expectedMonth)
	}

	// LastReset should be first of current month
	expectedReset := firstOfMonth(now)
	if !data.LastReset.Equal(expectedReset) {
		t.Errorf("LastReset = %v, want %v", data.LastReset, expectedReset)
	}
}

func TestCopilot_SaveAndLoadUsageData(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewCopilotWithPath(tmpDir)

	// Create test data
	now := time.Now().UTC()
	testData := &CopilotUsageData{
		RequestCount: 42,
		LastReset:    firstOfMonth(now),
		Month:        now.Format("2006-01"),
	}

	// Save
	if err := provider.SaveUsageData(testData); err != nil {
		t.Fatalf("SaveUsageData() error: %v", err)
	}

	// Verify file exists
	path := provider.usageFilePath()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("usage file not created: %v", err)
	}

	// Load
	loaded, err := provider.LoadUsageData()
	if err != nil {
		t.Fatalf("LoadUsageData() error: %v", err)
	}

	// Compare
	if loaded.RequestCount != testData.RequestCount {
		t.Errorf("RequestCount = %d, want %d", loaded.RequestCount, testData.RequestCount)
	}
	if loaded.Month != testData.Month {
		t.Errorf("Month = %q, want %q", loaded.Month, testData.Month)
	}
	if !loaded.LastReset.Equal(testData.LastReset) {
		t.Errorf("LastReset = %v, want %v", loaded.LastReset, testData.LastReset)
	}
}

func TestCopilot_GetRequestCount_CurrentMonth(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewCopilotWithPath(tmpDir)

	// Save test data for current month
	now := time.Now().UTC()
	testData := &CopilotUsageData{
		RequestCount: 15,
		LastReset:    firstOfMonth(now),
		Month:        now.Format("2006-01"),
	}
	if err := provider.SaveUsageData(testData); err != nil {
		t.Fatal(err)
	}

	// Get count
	count, err := provider.GetRequestCount()
	if err != nil {
		t.Fatalf("GetRequestCount() error: %v", err)
	}
	if count != 15 {
		t.Errorf("GetRequestCount() = %d, want 15", count)
	}
}

func TestCopilot_GetRequestCount_OldMonth(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewCopilotWithPath(tmpDir)

	// Save test data for previous month
	now := time.Now().UTC()
	lastMonth := now.AddDate(0, -1, 0)
	testData := &CopilotUsageData{
		RequestCount: 99,
		LastReset:    firstOfMonth(lastMonth),
		Month:        lastMonth.Format("2006-01"),
	}
	if err := provider.SaveUsageData(testData); err != nil {
		t.Fatal(err)
	}

	// Get count - should be reset to 0 for new month
	count, err := provider.GetRequestCount()
	if err != nil {
		t.Fatalf("GetRequestCount() error: %v", err)
	}
	if count != 0 {
		t.Errorf("GetRequestCount() = %d, want 0 (should reset for new month)", count)
	}

	// Verify data was updated
	loaded, err := provider.LoadUsageData()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Month != now.Format("2006-01") {
		t.Errorf("Month = %q, want %q (current month)", loaded.Month, now.Format("2006-01"))
	}
}

func TestCopilot_IncrementRequestCount(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewCopilotWithPath(tmpDir)

	// Start with 0
	count, err := provider.GetRequestCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("initial count = %d, want 0", count)
	}

	// Increment
	if err := provider.IncrementRequestCount(); err != nil {
		t.Fatalf("IncrementRequestCount() error: %v", err)
	}

	// Check count
	count, err = provider.GetRequestCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("count after increment = %d, want 1", count)
	}

	// Increment again
	if err := provider.IncrementRequestCount(); err != nil {
		t.Fatal(err)
	}

	count, err = provider.GetRequestCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count after second increment = %d, want 2", count)
	}
}


func TestCopilot_GetMonthlyResetTime(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewCopilotWithPath(tmpDir)

	resetTime := provider.GetMonthlyResetTime()

	// Should be first of next month at 00:00:00 UTC
	if resetTime.Hour() != 0 || resetTime.Minute() != 0 || resetTime.Second() != 0 {
		t.Errorf("reset time = %v, want 00:00:00", resetTime)
	}
	if resetTime.Day() != 1 {
		t.Errorf("reset day = %d, want 1", resetTime.Day())
	}
	if resetTime.Location() != time.UTC {
		t.Errorf("reset location = %v, want UTC", resetTime.Location())
	}

	// Should be in the future
	now := time.Now().UTC()
	if !resetTime.After(now) {
		t.Errorf("reset time %v should be after now %v", resetTime, now)
	}
}

func TestCopilot_GetResetTime(t *testing.T) {
	tmpDir := t.TempDir()
	provider := NewCopilotWithPath(tmpDir)

	tests := []struct {
		mode string
	}{
		{"daily"},
		{"weekly"},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			resetTime, err := provider.GetResetTime(tt.mode)
			if err != nil {
				t.Fatalf("GetResetTime(%q) error: %v", tt.mode, err)
			}

			// Should be first of next month
			if resetTime.Day() != 1 {
				t.Errorf("reset day = %d, want 1", resetTime.Day())
			}
		})
	}
}

func TestFirstOfMonth(t *testing.T) {
	tests := []struct {
		input    time.Time
		expected time.Time
	}{
		{
			time.Date(2026, 2, 15, 14, 30, 0, 0, time.UTC),
			time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		result := firstOfMonth(tt.input)
		if !result.Equal(tt.expected) {
			t.Errorf("firstOfMonth(%v) = %v, want %v", tt.input, result, tt.expected)
		}
	}
}

func TestDaysElapsedInMonth(t *testing.T) {
	tests := []struct {
		name    string
		now     time.Time
		wantMin float64
		wantMax float64
	}{
		{
			name:    "6 AM on day 1: rawElapsed=0.25, returned as-is (above 1h minimum)",
			now:     time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC),
			wantMin: 0.24,
			wantMax: 0.26,
		},
		{
			name:    "exact reset moment: clamped to 1h minimum",
			now:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			wantMin: 1.0 / 24.0,
			wantMax: 1.0/24.0 + 0.001,
		},
		{
			name:    "day 15 at noon: 14.5 days elapsed",
			now:     time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC),
			wantMin: 14.49,
			wantMax: 14.51,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := daysElapsedInMonth(tt.now)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("daysElapsedInMonth(%v) = %f, want [%f, %f]", tt.now, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

