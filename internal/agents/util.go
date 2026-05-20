package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// handleExecuteResult builds an ExecuteResult from raw runner output, applying
// uniform timeout detection, exit-code extraction, and JSON parsing. All three
// agent Execute() methods delegate their post-run logic here.
func handleExecuteResult(
	ctx context.Context,
	stdout, stderr string,
	exitCode int,
	err error,
	timeout time.Duration,
	start time.Time,
	compressStats *CompressStats,
	extractJSONFn func([]byte) []byte,
) (*ExecuteResult, error) {
	result := &ExecuteResult{
		Output:        stdout,
		CompressStats: compressStats,
		ExitCode:      exitCode,
		Duration:      time.Since(start),
	}

	if ctx.Err() == context.DeadlineExceeded {
		result.Error = fmt.Sprintf("timeout after %v", timeout)
		if stderr != "" {
			result.Error = fmt.Sprintf("timeout after %v; stderr: %s", timeout, truncate(stderr, 2000))
		}
		if stdout != "" {
			result.Output = stdout
		}
		result.ExitCode = -1
		return result, ctx.Err()
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
			result.Error = stderr
		} else {
			result.Error = err.Error()
			if stderr != "" {
				result.Error = fmt.Sprintf("%s; stderr: %s", err.Error(), truncate(stderr, 2000))
			}
		}
		return result, err
	}

	result.JSON = extractJSONFn([]byte(stdout))
	return result, nil
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
