package vulnidentity

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// Collect resolves all local replacement identities, including replacements in
// copied modules' standalone graphs. Explicit replacement versions describe the
// copied source even when another version is selected by the root module.
func Collect(ctx context.Context, root string) ([]string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}

	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolve repository root links: %w", err)
	}

	visited, identities := map[string]bool{}, map[string]bool{}

	queue := []string{root}
	for len(queue) > 0 {
		directory := queue[0]
		queue = queue[1:]

		if visited[directory] {
			continue
		}

		visited[directory] = true

		file, readErr := readModule(directory)
		if readErr != nil {
			return nil, readErr
		}

		for _, replacement := range file.Replace {
			if replacement.New.Version != "" {
				continue
			}

			identity, target, identityErr := localIdentity(ctx, root, directory, replacement)
			if identityErr != nil {
				return nil, identityErr
			}

			identities[identity] = true

			queue = append(queue, target)
		}
	}

	result := make([]string, 0, len(identities))
	for identity := range identities {
		result = append(result, identity)
	}

	slices.Sort(result)

	return result, nil
}

func readModule(directory string) (*modfile.File, error) {
	path := filepath.Join(directory, "go.mod")

	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("local module requires a regular go.mod: %s: %w", path, err)
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: local module requires a regular go.mod: %s", ErrIdentity, path)
	}

	contents, err := os.ReadFile(path) //nolint:gosec // Path is confined to the repository.
	if err != nil {
		return nil, fmt.Errorf("read local module: %w", err)
	}

	file, err := modfile.Parse(path, contents, nil)
	if err != nil {
		return nil, fmt.Errorf("parse local module: %w", err)
	}

	return file, nil
}

func replacementDirectory(root, directory, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(directory, path)
	}

	path = filepath.Clean(path)

	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: local replacement escapes the repository", ErrIdentity)
	}

	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path {
		return "", fmt.Errorf(
			"%w: local replacement is missing or contains a symbolic link",
			ErrIdentity,
		)
	}

	return path, nil
}

func selectedVersion(ctx context.Context, directory, path string) (string, error) {
	// The module path is an argument after --; no shell or executable is selected by it.
	//nolint:gosec // The executable is fixed; the module path follows --.
	command := exec.CommandContext(
		ctx,
		"go",
		"list",
		"-mod=readonly",
		"-m",
		"-json",
		"--",
		path,
	)
	command.Dir = directory
	// Resolve the parsed module, independently of an ambient workspace or modfile.
	command.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("resolve selected version for %s: %w", path, err)
	}
	// The Go command defines these JSON fields with initial capitals.
	//nolint:tagliatelle
	var selected struct {
		Path    string `json:"Path"`
		Version string `json:"Version"`
	}

	err = json.Unmarshal(output, &selected)
	if err != nil {
		return "", fmt.Errorf("invalid selected module observation: %w", err)
	}

	if selected.Path != path || selected.Version == "" {
		return "", fmt.Errorf("%w: missing selected published version for %s", ErrIdentity, path)
	}

	return selected.Version, nil
}

func localIdentity(
	ctx context.Context,
	root, directory string,
	replacement *modfile.Replace,
) (string, string, error) {
	target, err := replacementDirectory(root, directory, replacement.New.Path)
	if err != nil {
		return "", "", err
	}

	source, err := readModule(target)
	if err != nil {
		return "", "", err
	}

	if source.Module == nil || source.Module.Mod.Path != replacement.Old.Path {
		return "", "", fmt.Errorf(
			"%w: local source differs from %s",
			ErrIdentity,
			replacement.Old.Path,
		)
	}

	version := replacement.Old.Version
	if version == "" {
		version, err = selectedVersion(ctx, directory, replacement.Old.Path)
		if err != nil {
			return "", "", err
		}
	}

	err = module.Check(replacement.Old.Path, version)
	if err != nil {
		return "", "", fmt.Errorf("invalid published replacement identity: %w", err)
	}

	return replacement.Old.Path + "@" + version, target, nil
}
