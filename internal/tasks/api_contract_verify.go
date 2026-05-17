package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SpecCompareReport summarizes a semantic comparison between two API specs.
type SpecCompareReport struct {
	FileA     string `json:"file_a"`
	FileB     string `json:"file_b"`
	Identical bool   `json:"identical"`
	Summary   string `json:"summary"`
}

// FindAPISpecFiles walks projectPath and returns candidate API spec files
// (OpenAPI/Swagger YAML/JSON and .proto files). It skips common vendor dirs.
func FindAPISpecFiles(projectPath string) ([]string, error) {
	var matches []string
	walk := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "vendor" || base == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		name := strings.ToLower(d.Name())
		if strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".proto") {
			// heuristics: filenames mentioning openapi/swagger or common extensions
			if strings.Contains(name, "openapi") || strings.Contains(name, "swagger") || strings.HasSuffix(name, ".proto") || strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
				matches = append(matches, path)
			}
		}
		return nil
	}
	if err := filepath.WalkDir(projectPath, walk); err != nil {
		return nil, err
	}
	return matches, nil
}

// GenerateSpecIfPossible tries to run `swag init` in the projectPath and
// returns the output directory path. If `swag` is not present it returns "" and nil error.
// This is intentionally conservative: failures from `swag` are returned as errors so callers
// can decide whether to ignore them.
func GenerateSpecIfPossible(ctx context.Context, projectPath string) (string, error) {
	if _, err := exec.LookPath("swag"); err != nil {
		return "", nil
	}
	tmp, err := os.MkdirTemp("", "nightshift-swag-*")
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, "swag", "init", "-o", tmp)
	cmd.Dir = projectPath
	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tmp)
		return "", fmt.Errorf("swag init failed: %w: %s", err, string(out))
	}
	return tmp, nil
}

// CompareSpecs loads two spec files (JSON or YAML) and performs a semantic
// comparison that ignores ordering of map keys. The returned report summarizes
// whether the specs are identical and a brief summary of differences.
func CompareSpecs(pathA, pathB string) (*SpecCompareReport, error) {
	a, err := loadSpec(pathA)
	if err != nil {
		return nil, fmt.Errorf("load A: %w", err)
	}
	b, err := loadSpec(pathB)
	if err != nil {
		return nil, fmt.Errorf("load B: %w", err)
	}
	sa := canonicalString(a)
	sb := canonicalString(b)
	rep := &SpecCompareReport{FileA: pathA, FileB: pathB}
	if sa == sb {
		rep.Identical = true
		rep.Summary = "Specs are semantically identical"
		return rep, nil
	}
	rep.Identical = false
	ka := topLevelKeys(a)
	kb := topLevelKeys(b)
	onlyA := difference(ka, kb)
	onlyB := difference(kb, ka)
	s := []string{"Specs differ"}
	if len(onlyA) > 0 {
		s = append(s, fmt.Sprintf("keys only in A: %v", onlyA))
	}
	if len(onlyB) > 0 {
		s = append(s, fmt.Sprintf("keys only in B: %v", onlyB))
	}
	rep.Summary = strings.Join(s, "; ")
	return rep, nil
}

func loadSpec(path string) (interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var v interface{}
	if json.Unmarshal(data, &v) == nil {
		return v, nil
	}
	if yaml.Unmarshal(data, &v) == nil {
		return v, nil
	}
	// Fallback to raw bytes as string
	return string(data), nil
}

func canonicalString(v interface{}) string {
	switch t := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb := &strings.Builder{}
		sb.WriteString("{")
		for i, k := range keys {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(k)
			sb.WriteString(":")
			sb.WriteString(canonicalString(t[k]))
		}
		sb.WriteString("}")
		return sb.String()
	case map[interface{}]interface{}:
		// convert keys to strings
		m := make(map[string]interface{}, len(t))
		keys := make([]string, 0, len(t))
		for kk, vv := range t {
			ks := fmt.Sprintf("%v", kk)
			keys = append(keys, ks)
			m[ks] = vv
		}
		sort.Strings(keys)
		sb := &strings.Builder{}
		sb.WriteString("{")
		for i, k := range keys {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(k)
			sb.WriteString(":")
			sb.WriteString(canonicalString(m[k]))
		}
		sb.WriteString("}")
		return sb.String()
	case []interface{}:
		sb := &strings.Builder{}
		sb.WriteString("[")
		for i, el := range t {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString(canonicalString(el))
		}
		sb.WriteString("]")
		return sb.String()
	default:
		return fmt.Sprintf("%#v", t)
	}
}

func topLevelKeys(v interface{}) []string {
	switch t := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return keys
	case map[interface{}]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, fmt.Sprintf("%v", k))
		}
		sort.Strings(keys)
		return keys
	default:
		return nil
	}
}

func difference(a, b []string) []string {
	mb := make(map[string]struct{}, len(b))
	for _, s := range b {
		mb[s] = struct{}{}
	}
	out := make([]string, 0)
	for _, s := range a {
		if _, ok := mb[s]; !ok {
			out = append(out, s)
		}
	}
	return out
}
