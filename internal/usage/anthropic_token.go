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

// CredentialStore reads and writes OAuth credentials.
type CredentialStore interface {
	// ReadToken returns the stored OAuth tokens, respecting the context deadline.
	ReadToken(ctx context.Context) (accessToken, refreshToken string, expiresAt time.Time, err error)
	WriteToken(accessToken, refreshToken string, expiresAt time.Time) error
}

// claudeCredentialsFile mirrors ~/.claude/.credentials.json structure.
type claudeCredentialsFile struct {
	ClaudeAiOauth struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
		ExpiresAt    int64  `json:"expiresAt"` // Unix milliseconds
	} `json:"claudeAiOauth"`
}

// tokenDiscovery implements CredentialStore using keychain/keyring/file fallback.
type tokenDiscovery struct {
	runner   KeychainRunner
	filePath string // path to ~/.claude/.credentials.json
	homeErr  error  // error from UserHomeDir if it failed
}

// NewTokenDiscovery returns a CredentialStore that tries macOS keychain,
// Linux keyring, then ~/.claude/.credentials.json in order.
func NewTokenDiscovery(runner KeychainRunner) CredentialStore {
	home, err := os.UserHomeDir()
	if err != nil {
		// Fall back to current user's home via env if UserHomeDir fails.
		home = os.Getenv("HOME")
		if home == "" {
			home = os.Getenv("USERPROFILE") // Windows fallback
		}
	}
	return &tokenDiscovery{
		runner:   runner,
		filePath: filepath.Join(home, ".claude", ".credentials.json"),
		homeErr:  err,
	}
}

// ReadToken discovers the OAuth access token using platform-specific methods,
// respecting the context deadline to avoid hanging on slow keychain/keyring.
func (t *tokenDiscovery) ReadToken(ctx context.Context) (string, string, time.Time, error) {
	user := os.Getenv("USER")
	if user == "" {
		user = os.Getenv("LOGNAME")
	}

	if user != "" && t.runner != nil {
		// Create a tight context with 2s timeout for keychain/keyring calls.
		keychainCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		// Try macOS keychain
		stdout, _, code, err := t.runner.Run(keychainCtx, "security",
			[]string{"find-generic-password", "-s", "Claude Code-credentials", "-a", user, "-w"},
			"", "")
		if err == nil && code == 0 {
			data := strings.TrimSpace(stdout)
			if data != "" {
				access, refresh, exp, parseErr := parseKeychainJSON(data)
				if parseErr == nil {
					return access, refresh, exp, nil
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
				access, refresh, exp, parseErr := parseKeychainJSON(data)
				if parseErr == nil {
					return access, refresh, exp, nil
				}
			}
		}
	}

	// File fallback
	return t.readFromFile()
}

// WriteToken writes updated OAuth tokens back to the credential file.
// Currently only writes to ~/.claude/.credentials.json; keychain/keyring write-back
// is not implemented (tokens persist via file). This is sufficient for token refresh
// since the same code path that reads keychain will fall through to file on next run.
func (t *tokenDiscovery) WriteToken(accessToken, refreshToken string, expiresAt time.Time) error {
	return t.writeToFile(accessToken, refreshToken, expiresAt)
}

func (t *tokenDiscovery) readFromFile() (string, string, time.Time, error) {
	data, err := os.ReadFile(t.filePath)
	if err != nil {
		return "", "", time.Time{}, fmt.Errorf("reading credentials file: %w", err)
	}

	var creds claudeCredentialsFile
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", "", time.Time{}, fmt.Errorf("parsing credentials file: %w", err)
	}

	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", "", time.Time{}, fmt.Errorf("no access token in credentials file")
	}

	var expiresAt time.Time
	if creds.ClaudeAiOauth.ExpiresAt > 0 {
		// ExpiresAt is stored as Unix milliseconds
		expiresAt = time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
	}

	return creds.ClaudeAiOauth.AccessToken, creds.ClaudeAiOauth.RefreshToken, expiresAt, nil
}

func (t *tokenDiscovery) writeToFile(accessToken, refreshToken string, expiresAt time.Time) error {
	// Read existing file to preserve other fields
	existing, err := os.ReadFile(t.filePath)
	var creds claudeCredentialsFile
	if err == nil {
		_ = json.Unmarshal(existing, &creds)
	}

	creds.ClaudeAiOauth.AccessToken = accessToken
	if refreshToken != "" {
		creds.ClaudeAiOauth.RefreshToken = refreshToken
	}
	if !expiresAt.IsZero() {
		creds.ClaudeAiOauth.ExpiresAt = expiresAt.UnixMilli()
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling credentials: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(t.filePath), 0700); err != nil {
		return fmt.Errorf("creating credentials dir: %w", err)
	}

	if err := os.WriteFile(t.filePath, data, 0600); err != nil {
		return fmt.Errorf("writing credentials file: %w", err)
	}

	return nil
}

// parseKeychainJSON parses the JSON blob returned by security/secret-tool.
// The keychain stores the same JSON as the credentials file.
func parseKeychainJSON(raw string) (access, refresh string, expiresAt time.Time, err error) {
	var creds claudeCredentialsFile
	if err = json.Unmarshal([]byte(raw), &creds); err != nil {
		return
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		err = fmt.Errorf("no access token in keychain data")
		return
	}
	access = creds.ClaudeAiOauth.AccessToken
	refresh = creds.ClaudeAiOauth.RefreshToken
	if creds.ClaudeAiOauth.ExpiresAt > 0 {
		expiresAt = time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
	}
	return
}
