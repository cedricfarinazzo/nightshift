package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// buildFileContext reads the given files and formats them as a markdown context
// block suitable for injection into an agent prompt.
func buildFileContext(files []string) (string, error) {
	var sb strings.Builder
	sb.WriteString("# Context Files\n\n")
	for _, path := range files {
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", path, err)
		}
		displayPath := path
		if abs, err := filepath.Abs(path); err == nil {
			displayPath = abs
		}
		fmt.Fprintf(&sb, "## File: %s\n\n```\n%s\n```\n\n", displayPath, string(content))
	}
	return sb.String(), nil
}

// extractJSON attempts to find and parse JSON from raw output bytes.
// Returns nil if no valid JSON is found.
func extractJSON(output []byte) []byte {
	if json.Valid(output) {
		return output
	}

	start := -1
	var opener, closer byte
	for i, b := range output {
		if b == '{' || b == '[' {
			start = i
			opener = b
			if b == '{' {
				closer = '}'
			} else {
				closer = ']'
			}
			break
		}
	}
	if start == -1 {
		return nil
	}

	depth := 0
	for i := start; i < len(output); i++ {
		if output[i] == opener {
			depth++
		} else if output[i] == closer {
			depth--
			if depth == 0 {
				candidate := output[start : i+1]
				if json.Valid(candidate) {
					return candidate
				}
				break
			}
		}
	}
	return nil
}
