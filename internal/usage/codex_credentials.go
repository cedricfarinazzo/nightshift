package usage

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CodexCredentials holds OAuth tokens for the Codex/ChatGPT API.
type CodexCredentials struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	UserID       string
	ExpiresAt    time.Time
	AuthFilePath string
}

// IsExpired reports whether the access token is expired or expiring within 30s.
func (c *CodexCredentials) IsExpired() bool {
	return !c.ExpiresAt.IsZero() && time.Now().After(c.ExpiresAt.Add(-30*time.Second))
}

// authFileShape mirrors the structure of ~/.codex/auth.json.
type authFileShape struct {
	Tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	} `json:"tokens"`
}

// LoadCredentials discovers Codex OAuth credentials in order:
//  1. CODEX_TOKEN env var
//  2. $CODEX_HOME/auth.json
//  3. ~/.codex/auth.json
func LoadCredentials() (*CodexCredentials, error) {
	if tok := os.Getenv("CODEX_TOKEN"); tok != "" {
		return &CodexCredentials{AccessToken: tok}, nil
	}

	path, err := authFilePath()
	if err != nil {
		return nil, ErrNoCredentials
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, ErrNoCredentials
	}

	var af authFileShape
	if err := json.Unmarshal(data, &af); err != nil {
		return nil, fmt.Errorf("codex: parse auth.json: %w", err)
	}

	if af.Tokens.AccessToken == "" {
		return nil, ErrNoCredentials
	}

	creds := &CodexCredentials{
		AccessToken:  af.Tokens.AccessToken,
		RefreshToken: af.Tokens.RefreshToken,
		IDToken:      af.Tokens.IDToken,
		AuthFilePath: path,
	}

	if af.Tokens.IDToken != "" {
		if userID, exp, err := parseIDToken(af.Tokens.IDToken); err == nil {
			creds.UserID = userID
			creds.ExpiresAt = exp
		}
	}

	return creds, nil
}

func authFilePath() (string, error) {
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return filepath.Join(home, "auth.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "auth.json"), nil
}

// parseIDToken decodes the JWT middle segment and extracts exp + chatgpt_user_id.
// Signature is NOT verified — we only need the claims.
func parseIDToken(token string) (userID string, exp time.Time, err error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", time.Time{}, fmt.Errorf("codex: malformed id_token")
	}

	// Base64url decode without padding
	payload := parts[1]
	switch len(payload) % 4 {
	case 2:
		payload += "=="
	case 3:
		payload += "="
	}
	decoded, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		// Try RawURLEncoding
		decoded, err = base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return "", time.Time{}, fmt.Errorf("codex: decode id_token payload: %w", err)
		}
	}

	var claims struct {
		Exp            int64  `json:"exp"`
		ChatGPTUserID  string `json:"chatgpt_user_id"`
		Sub            string `json:"sub"`
	}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "", time.Time{}, fmt.Errorf("codex: parse id_token claims: %w", err)
	}

	if claims.Exp > 0 {
		exp = time.Unix(claims.Exp, 0)
	}

	userID = claims.ChatGPTUserID
	if userID == "" {
		userID = claims.Sub
	}

	return userID, exp, nil
}
