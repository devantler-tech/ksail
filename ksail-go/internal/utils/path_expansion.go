package utils

import (
	"os/user"
	"path/filepath"
	"strings"
)

// ExpandPath expands the given path, replacing the home directory shortcut with the full path.
func ExpandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		usr, err := user.Current()
		if err != nil {
			return "", err
		}
		return filepath.Join(usr.HomeDir, path[2:]), nil
	}
	return path, nil
}
