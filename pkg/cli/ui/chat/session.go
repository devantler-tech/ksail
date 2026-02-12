package chat

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	copilot "github.com/github/copilot-sdk/go"
)

const (
	// sessionDirPerm is the permission for the sessions directory.
	sessionDirPerm = 0o700
	// sessionFilePerm is the permission for session files.
	sessionFilePerm = 0o600
	// maxSessionNameLength is the maximum length for generated session names.
	maxSessionNameLength = 40
	// truncatedNameSuffix is added when session names are truncated.
	truncatedNameSuffix = "..."
	// defaultSessionName is used when no name can be generated.
	defaultSessionName = "New Chat"
	// unnamedSessionName is used when SDK has no summary and no local name.
	unnamedSessionName = "Unnamed"
	// hoursPerDay is the number of hours in a day.
	hoursPerDay = 24
)

// Sentinel errors for session operations.
var (
	errSessionIDEmpty   = errors.New("session ID is empty")
	errInvalidSessionID = errors.New("invalid session ID")
)

// MessageMetadata represents metadata for a single message in a session.
type MessageMetadata struct {
	AgentMode bool `json:"agentMode"` // true = agent mode, false = plan mode
}

// SessionMetadata represents metadata for a chat session stored locally.
// Message content is stored server-side by Copilot and retrieved via ResumeSession.
type SessionMetadata struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Model     string `json:"model,omitempty"`
	AgentMode *bool  `json:"agentMode,omitempty"` // nil or true = agent mode, false = plan mode
	// Messages stores per-message metadata (agentMode for each user message).
	Messages    []MessageMetadata        `json:"messages,omitempty"`
	SDKMetadata *copilot.SessionMetadata `json:"-"` // SDK metadata (not persisted)
	CreatedAt   time.Time                `json:"createdAt"`
	UpdatedAt   time.Time                `json:"updatedAt"`
}

// GetDisplayName returns the display name using hierarchy: Local Name → SDK Summary → "Unnamed".
// User-defined names take precedence over SDK-generated summaries.
func (s *SessionMetadata) GetDisplayName() string {
	if s.Name != "" {
		return s.Name
	}

	if s.SDKMetadata != nil && s.SDKMetadata.Summary != nil && *s.SDKMetadata.Summary != "" {
		return *s.SDKMetadata.Summary
	}

	return unnamedSessionName
}

// sessionsDir returns the path to the chat sessions directory.
func sessionsDir(appDir string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	return filepath.Join(home, appDir, "chat", "sessions"), nil
}

// ensureSessionsDir creates the sessions directory if it doesn't exist.
func ensureSessionsDir(appDir string) (string, error) {
	dir, err := sessionsDir(appDir)
	if err != nil {
		return "", err
	}

	err = os.MkdirAll(dir, sessionDirPerm)
	if err != nil {
		return "", fmt.Errorf("failed to create sessions directory: %w", err)
	}

	return dir, nil
}

// ListSessions returns all local session metadata from SDK, enriched with local metadata.
// Remote sessions are excluded from the returned slice.
// Sessions are sorted by ModifiedTime (most recent first).
func ListSessions(client *copilot.Client, appDir string) ([]SessionMetadata, error) {
	// Get sessions from SDK
	sdkSessions, err := client.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("failed to list SDK sessions: %w", err)
	}

	sessions := make([]SessionMetadata, 0, len(sdkSessions))
	for i := range sdkSessions {
		sdkSession := &sdkSessions[i]
		if sdkSession.IsRemote {
			continue // Skip remote sessions
		}

		// Try to load local metadata
		localSession, err := LoadSession(sdkSession.SessionID, appDir)
		if err != nil {
			// No local metadata, create from SDK only
			localSession = &SessionMetadata{
				ID:   sdkSession.SessionID,
				Name: "", // Will use SDK Summary or "Unnamed" for display
			}
		}

		// Attach SDK metadata
		localSession.SDKMetadata = sdkSession

		sessions = append(sessions, *localSession)
	}

	// Sort by SDK ModifiedTime descending (most recent first)
	slices.SortFunc(sessions, func(a, b SessionMetadata) int {
		return cmp.Compare(b.SDKMetadata.ModifiedTime, a.SDKMetadata.ModifiedTime)
	})

	return sessions, nil
}

// validateSessionID ensures the session ID contains only safe characters.
// Allowed characters are ASCII letters, digits, hyphens, and underscores.
// This prevents path traversal attacks using malicious session IDs.
func validateSessionID(
	sessionID string,
) error {
	if sessionID == "" {
		return errSessionIDEmpty
	}

	for _, char := range sessionID {
		if isValidSessionIDChar(char) {
			continue
		}

		return fmt.Errorf(
			"%w: %q contains invalid character %q",
			errInvalidSessionID,
			sessionID,
			char,
		)
	}

	return nil
}

// isValidSessionIDChar returns true if the rune is allowed in session IDs.
func isValidSessionIDChar(char rune) bool {
	return (char >= 'a' && char <= 'z') ||
		(char >= 'A' && char <= 'Z') ||
		(char >= '0' && char <= '9') ||
		char == '-' || char == '_'
}

// LoadSession loads session metadata by ID.
func LoadSession(
	sessionID string,
	appDir string,
) (*SessionMetadata, error) {
	err := validateSessionID(sessionID)
	if err != nil {
		return nil, err
	}

	dir, err := sessionsDir(appDir)
	if err != nil {
		return nil, err
	}

	path := filepath.Join(dir, sessionID+".json")

	data, err := os.ReadFile(path) //nolint:gosec // Path is validated via validateSessionID
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	var session SessionMetadata

	err = json.Unmarshal(data, &session)
	if err != nil {
		return nil, fmt.Errorf("failed to parse session file: %w", err)
	}

	return &session, nil
}

// SaveSession saves session metadata to disk.
// The ID must be provided (typically from the Copilot SDK session.SessionID).
func SaveSession(session *SessionMetadata, appDir string) error {
	err := validateSessionID(session.ID)
	if err != nil {
		return err
	}

	dir, err := ensureSessionsDir(appDir)
	if err != nil {
		return err
	}

	// Set timestamps
	if session.CreatedAt.IsZero() {
		session.CreatedAt = time.Now()
	}

	session.UpdatedAt = time.Now()

	if session.Name == "" {
		session.Name = defaultSessionName
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize session: %w", err)
	}

	path := filepath.Join(dir, session.ID+".json")

	err = os.WriteFile(path, data, sessionFilePerm)
	if err != nil {
		return fmt.Errorf("failed to write session file: %w", err)
	}

	return nil
}

// DeleteSession removes a chat session from both SDK and local disk.
func DeleteSession(
	client *copilot.Client,
	sessionID string,
	appDir string,
) error {
	err := validateSessionID(sessionID)
	if err != nil {
		return err
	}

	// Delete from SDK first
	err = client.DeleteSession(sessionID)
	if err != nil {
		return fmt.Errorf("failed to delete SDK session: %w", err)
	}

	// Then delete local file
	return deleteLocalSession(sessionID, appDir)
}

// deleteLocalSession removes a chat session from local disk only.
func deleteLocalSession(
	sessionID string,
	appDir string,
) error {
	err := validateSessionID(sessionID)
	if err != nil {
		return err
	}

	dir, err := sessionsDir(appDir)
	if err != nil {
		return err
	}

	path := filepath.Join(dir, sessionID+".json")

	removeErr := os.Remove(path)
	if removeErr != nil {
		if os.IsNotExist(removeErr) {
			return nil // Already deleted
		}

		return fmt.Errorf("failed to delete session file: %w", removeErr)
	}

	return nil
}

// SyncSessionMetadata synchronizes local session files with SDK sessions.
// It removes local session files that no longer exist in the SDK (excluding remote sessions).
// Returns an error if sync fails (caller should display as non-blocking toast).
func SyncSessionMetadata(client *copilot.Client, appDir string) error {
	// Get all sessions from SDK
	sdkSessions, err := client.ListSessions()
	if err != nil {
		return fmt.Errorf("failed to list SDK sessions: %w", err)
	}

	// Build set of valid local SDK session IDs (excluding remote sessions)
	validIDs := make(map[string]struct{})

	for _, sdkSession := range sdkSessions {
		if sdkSession.IsRemote {
			continue // Skip remote sessions
		}

		validIDs[sdkSession.SessionID] = struct{}{}
	}

	// Get local sessions directory
	dir, err := sessionsDir(appDir)
	if err != nil {
		return err
	}

	return deleteOrphanedLocalSessions(dir, validIDs)
}

// deleteOrphanedLocalSessions removes local session files not present in the valid IDs set.
func deleteOrphanedLocalSessions(dir string, validIDs map[string]struct{}) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No local sessions to sync
		}

		return fmt.Errorf("failed to read sessions directory: %w", err)
	}

	// Delete local files that don't exist in SDK
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		sessionID := strings.TrimSuffix(entry.Name(), ".json")
		if _, exists := validIDs[sessionID]; !exists {
			// Local session doesn't exist in SDK, delete it
			path := filepath.Join(dir, entry.Name())

			removeErr := os.Remove(path)
			if removeErr != nil && !os.IsNotExist(removeErr) {
				// Log error but continue with other files
				continue
			}
		}
	}

	return nil
}

// ErrNoSessions is returned when there are no sessions available.
var ErrNoSessions = errors.New("no sessions available")

// GetMostRecentSession returns the most recently updated session metadata.
// Returns ErrNoSessions if no sessions exist.
func GetMostRecentSession(client *copilot.Client, appDir string) (*SessionMetadata, error) {
	sessions, err := ListSessions(client, appDir)
	if err != nil {
		return nil, err
	}

	if len(sessions) == 0 {
		return nil, ErrNoSessions
	}

	return &sessions[0], nil
}

// GenerateSessionName creates a session name from the first user message.
func GenerateSessionName(messages []message) string {
	for _, msg := range messages {
		if msg.role == roleUser && msg.content != "" {
			name := strings.ReplaceAll(msg.content, "\n", " ")
			if utf8.RuneCountInString(name) > maxSessionNameLength {
				runes := []rune(name)
				name = string(
					runes[:maxSessionNameLength-len(truncatedNameSuffix)],
				) + truncatedNameSuffix
			}

			return name
		}
	}

	return defaultSessionName
}

// FormatRelativeTime formats a time as a relative string (e.g., "2 hours ago").
func FormatRelativeTime(timestamp time.Time) string {
	now := time.Now()
	diff := now.Sub(timestamp)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}

		return fmt.Sprintf("%d mins ago", mins)
	case diff < hoursPerDay*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}

		return fmt.Sprintf("%d hours ago", hours)
	case diff < 7*hoursPerDay*time.Hour:
		days := int(diff.Hours() / hoursPerDay)
		if days == 1 {
			return "yesterday"
		}

		return fmt.Sprintf("%d days ago", days)
	default:
		return timestamp.Format("Jan 2")
	}
}
