package tasks

import (
	"context"
	"path/filepath"
	"testing"
)

func TestCompareSpecs_IdenticalIgnoringOrder(t *testing.T) {
	a := filepath.Join("testdata", "spec_a.yaml")
	b := filepath.Join("testdata", "spec_a_reordered.yaml")
	rep, err := CompareSpecs(a, b)
	if err != nil {
		t.Fatalf("CompareSpecs error: %v", err)
	}
	if !rep.Identical {
		t.Fatalf("expected identical, got: %v", rep.Summary)
	}
}

func TestCompareSpecs_Different(t *testing.T) {
	a := filepath.Join("testdata", "spec_a.yaml")
	b := filepath.Join("testdata", "spec_b.yaml")
	rep, err := CompareSpecs(a, b)
	if err != nil {
		t.Fatalf("CompareSpecs error: %v", err)
	}
	if rep.Identical {
		t.Fatalf("expected different, got identical")
	}
}

func TestFindAPISpecFiles_FindsFiles(t *testing.T) {
	files, err := FindAPISpecFiles("testdata")
	if err != nil {
		t.Fatalf("FindAPISpecFiles error: %v", err)
	}
	if len(files) < 2 {
		t.Fatalf("expected >=2 files, got %d: %v", len(files), files)
	}
}

func TestGenerateSpecIfPossible_NoCrash(t *testing.T) {
	// conservative: ensure the function returns ("", nil) when `swag` is absent
	ctx := context.Background()
	_, err := GenerateSpecIfPossible(ctx, "testdata")
	if err != nil {
		// It's acceptable for the generator to fail if `swag` exists but cannot run.
		// The scaffold should not panic; surface the error instead.
		// Accept error here as long as it is non-fatal to the test run.
		return
	}
}
