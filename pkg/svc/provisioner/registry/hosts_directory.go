package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HostsDirectoryManager manages the containerd hosts directory structure for registry mirrors.
// It creates temporary directories with hosts.toml files that can be mounted into Kind nodes.
type HostsDirectoryManager struct {
	baseDir string
}

// NewHostsDirectoryManager creates a new manager for containerd hosts directory.
// The baseDir will be created if it doesn't exist.
func NewHostsDirectoryManager(baseDir string) (*HostsDirectoryManager, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("baseDir cannot be empty")
	}

	err := os.MkdirAll(baseDir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("failed to create base directory %s: %w", baseDir, err)
	}

	return &HostsDirectoryManager{
		baseDir: baseDir,
	}, nil
}

// WriteHostsToml creates a hosts.toml file for the specified registry mirror.
// Returns the path to the created directory (parent of hosts.toml).
func (m *HostsDirectoryManager) WriteHostsToml(entry MirrorEntry) (string, error) {
	// Create directory for this registry host
	registryDir := filepath.Join(m.baseDir, entry.Host)
	err := os.MkdirAll(registryDir, 0o755)
	if err != nil {
		return "", fmt.Errorf("failed to create registry directory %s: %w", registryDir, err)
	}

	// Generate hosts.toml content
	content := GenerateHostsToml(entry)

	// Write hosts.toml file
	hostsPath := filepath.Join(registryDir, "hosts.toml")
	err = os.WriteFile(hostsPath, []byte(content), 0o644)
	if err != nil {
		return "", fmt.Errorf("failed to write hosts.toml to %s: %w", hostsPath, err)
	}

	return registryDir, nil
}

// WriteAllHostsToml creates hosts.toml files for all provided mirror entries.
// Returns a map of registry host to directory path.
func (m *HostsDirectoryManager) WriteAllHostsToml(entries []MirrorEntry) (map[string]string, error) {
	result := make(map[string]string, len(entries))

	for _, entry := range entries {
		dir, err := m.WriteHostsToml(entry)
		if err != nil {
			return nil, fmt.Errorf("failed to write hosts.toml for %s: %w", entry.Host, err)
		}
		result[entry.Host] = dir
	}

	return result, nil
}

// Cleanup removes all created directories and files.
func (m *HostsDirectoryManager) Cleanup() error {
	if m.baseDir == "" {
		return nil
	}

	err := os.RemoveAll(m.baseDir)
	if err != nil {
		return fmt.Errorf("failed to cleanup hosts directory %s: %w", m.baseDir, err)
	}

	return nil
}

// GetBaseDir returns the base directory path.
func (m *HostsDirectoryManager) GetBaseDir() string {
	return m.baseDir
}

// ReadExistingHostsToml reads existing hosts.toml files from the kind-mirrors directory
// and returns MirrorSpec entries. This allows inferring mirror registry configuration
// from previously scaffolded hosts files.
func ReadExistingHostsToml(baseDir string) ([]MirrorSpec, error) {
	// Check if directory exists
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		return nil, nil // No existing config
	}

	// Read directory entries
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory %s: %w", baseDir, err)
	}

	var specs []MirrorSpec

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		host := entry.Name()
		hostsFilePath := filepath.Join(baseDir, host, "hosts.toml")

		// Check if hosts.toml exists
		if _, err := os.Stat(hostsFilePath); os.IsNotExist(err) {
			continue
		}

		// Read hosts.toml file
		content, err := os.ReadFile(hostsFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", hostsFilePath, err)
		}

		// Parse the server URL from hosts.toml
		remote := parseServerFromHostsToml(string(content))
		if remote == "" {
			// Skip if we can't find the server URL
			continue
		}

		specs = append(specs, MirrorSpec{
			Host:   host,
			Remote: remote,
		})
	}

	return specs, nil
}

// parseServerFromHostsToml extracts the server URL from hosts.toml content.
// Example: server = "https://registry-1.docker.io"
func parseServerFromHostsToml(content string) string {
	lines := strings.Split(content, "\n")
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		
		// Look for server = "url" pattern
		if strings.HasPrefix(line, "server") && strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				continue
			}
			
			value := strings.TrimSpace(parts[1])
			// Remove quotes
			value = strings.Trim(value, `"'`)
			
			return value
		}
	}
	
	return ""
}
