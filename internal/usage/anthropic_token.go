package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// KeychainRunner executes keychain/keyring commands. Allows mocking in tests.
type KeychainRunner interface {
	Run(ctx context.Context, name string, args []string, dir string, stdin string) (stdout, stderr string, exitCode int, err error)
}

// CredentialStore reads OAuth credentials.
type CredentialStore interface {
	// ReadToken returns the stored OAuth access token, respecting the context deadline.
	ReadToken(ctx context.Context) (accessToken string, err error)
}

// claudeCredentialsFile mirrors ~/.claude/.credentials.json structure.
type claudeCredentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

// tokenDiscovery implements CredentialStore using keychain/keyring/file fallback.
type tokenDiscovery struct {
	runner   KeychainRunner
	filePath string // path to ~/.claude/.credentials.json
}

// NewTokenDiscovery returns a CredentialStore that tries macOS keychain,
// Linux keyring, then ~/.claude/.credentials.json in order.
func NewTokenDiscovery(runner KeychainRunner) CredentialStore {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
	}
	return &tokenDiscovery{
		runner:   runner,
		filePath: filepath.Join(home, ".claude", ".credentials.json"),
	}
}

// ReadToken discovers the OAuth access token using platform-specific methods,
// respecting the context deadline to avoid hanging on slow keychain/keyring.
func (t *tokenDiscovery) ReadToken(ctx context.Context) (string, error) {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}

	if user != "" && t.runner != nil {
		keychainCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		// Try macOS keychain
		stdout, _, code, err := t.runner.Run(keychainCtx, "security",
			[]string{"find-generic-password", "-s", "Claude Code-credentials", "-a", user, "-w"},
			"", "")
		if err == nil && code == 0 {
			data := strings.TrimSpace(stdout)
			if data != "" {
				if access, parseErr := parseKeychainJSON(data); parseErr == nil {
					return access, nil
				}
			}
		}

		// Try Linux keyring
		stdout, _, code, err = t.runner.Run(keychainCtx, "secret-tool",
			[]string{"lookup", "service", "Claude Code-credentials", "account", user},
			"", "")
		if err == nil && code == 0 {
			data := strings.TrimSpace(stdout)
			if data != "" {
				if access, parseErr := parseKeychainJSON(data); parseErr == nil {
					return access, nil
				}
			}
		}
	}

	return t.readFromFile()
}

func (t *tokenDiscovery) readFromFile() (string, error) {
	data, err := os.ReadFile(t.filePath)
	if err != nil {
		return "", fmt.Errorf("reading credentials file: %w", err)
	}

	var creds claudeCredentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("parsing credentials file: %w", err)
	}

	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", fmt.Errorf("no access token in credentials file")
	}

	return creds.ClaudeAiOauth.AccessToken, nil
}

// parseKeychainJSON parses the JSON blob returned by security/secret-tool.
func parseKeychainJSON(raw string) (string, error) {
	var creds claudeCredentialsFile
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		return "", err
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", fmt.Errorf("no access token in keychain data")
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}
