package chat

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// SessionMetadata represents metadata for a chat session stored locally.
// Message content is stored server-side by Copilot and retrieved via ResumeSession.
type SessionMetadata struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Model     string    `json:"model,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// sessionsDir returns the path to the chat sessions directory.
func sessionsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(home, ".ksail", "chat", "sessions"), nil
}

// ensureSessionsDir creates the sessions directory if it doesn't exist.
func ensureSessionsDir() (string, error) {
	dir, err := sessionsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create sessions directory: %w", err)
	}
	return dir, nil
}

// ListSessions returns all saved session metadata, sorted by UpdatedAt (most recent first).
func ListSessions() ([]SessionMetadata, error) {
	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []SessionMetadata{}, nil
		}
		return nil, fmt.Errorf("failed to read sessions directory: %w", err)
	}

	sessions := make([]SessionMetadata, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		session, err := LoadSession(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil {
			// Skip corrupted sessions
			continue
		}
		sessions = append(sessions, *session)
	}

	// Sort by UpdatedAt descending (most recent first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

// validateSessionID ensures the session ID contains only safe characters.
// Allowed characters are ASCII letters, digits, hyphens, and underscores.
// This prevents path traversal attacks using malicious session IDs.
func validateSessionID(id string) error {
	if id == "" {
		return errors.New("session ID is empty")
	}
	for _, c := range id {
		if (c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' {
			continue
		}
		return fmt.Errorf("invalid session ID %q: contains invalid character %q", id, c)
	}
	return nil
}

// LoadSession loads session metadata by ID.
func LoadSession(id string) (*SessionMetadata, error) {
	if err := validateSessionID(id); err != nil {
		return nil, err
	}

	dir, err := sessionsDir()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, id+".json")
	data, err := os.ReadFile(path) //nolint:gosec // Path is validated via validateSessionID
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var session SessionMetadata
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to parse session file: %w", err)
	}

	return &session, nil
}

// SaveSession saves session metadata to disk.
// The ID must be provided (typically from the Copilot SDK session.SessionID).
func SaveSession(session *SessionMetadata) error {
	if err := validateSessionID(session.ID); err != nil {
		return err
	}

	dir, err := ensureSessionsDir()
	if err != nil {
		return err
	}

	// Set timestamps
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}
	session.UpdatedAt = time.Now()

	// Name should be set by caller from first user message
	if session.Name == "" {
		session.Name = "New Chat"
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize session: %w", err)
	}

	path := filepath.Join(dir, session.ID+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	return nil
}

// DeleteSession removes a chat session from disk.
func DeleteSession(id string) error {
	if err := validateSessionID(id); err != nil {
		return err
	}

	dir, err := sessionsDir()
	if err != nil {
		return err
	}

	path := filepath.Join(dir, id+".json")
	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return nil // Already deleted
		}
		return fmt.Errorf("failed to delete session file: %w", err)
	}

	return nil
}

// ErrNoSessions is returned when there are no sessions available.
var ErrNoSessions = errors.New("no sessions available")

// GetMostRecentSession returns the most recently updated session metadata.
// Returns ErrNoSessions if no sessions exist.
func GetMostRecentSession() (*SessionMetadata, error) {
	sessions, err := ListSessions()
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, ErrNoSessions
	}
	return &sessions[0], nil
}

// GenerateSessionName creates a session name from the first user message.
func GenerateSessionName(messages []chatMessage) string {
	for _, msg := range messages {
		if msg.role == "user" && msg.content != "" {
			// Remove newlines first, then truncate
			name := strings.ReplaceAll(msg.content, "\n", " ")
			// Truncate to ~40 runes (Unicode-safe)
			if utf8.RuneCountInString(name) > 40 {
				runes := []rune(name)
				name = string(runes[:37]) + "..."
			}
			return name
		}
	}
	return "New Chat"
}

// FormatRelativeTime formats a time as a relative string (e.g., "2 hours ago").
func FormatRelativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	default:
		return t.Format("Jan 2")
	}
}
