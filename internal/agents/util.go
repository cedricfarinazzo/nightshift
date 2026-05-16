package agents

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// buildFileContext formats the given file paths as a markdown list for injection
// into an agent prompt. The agent reads files directly from disk as needed.
func buildFileContext(files []string) string {
	var sb strings.Builder
	sb.WriteString("# Related Files\n\n")
	for _, path := range files {
		displayPath := path
		if abs, err := filepath.Abs(path); err == nil {
			displayPath = abs
		}
		fmt.Fprintf(&sb, "- %s\n", displayPath)
	}
	return sb.String()
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
