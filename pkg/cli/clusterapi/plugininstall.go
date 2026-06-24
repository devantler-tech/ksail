package clusterapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

const (
	// maxPluginDownloadBytes caps the compressed tarball download to bound memory/disk.
	maxPluginDownloadBytes = 64 << 20 // 64 MiB
	// maxPluginExtractedBytes caps total extracted bytes to defeat decompression bombs.
	maxPluginExtractedBytes = 256 << 20 // 256 MiB
	// maxPluginFiles caps the number of archive entries extracted.
	maxPluginFiles = 4096
	// pluginDownloadTimeout bounds the whole download.
	pluginDownloadTimeout = 60 * time.Second
	// pluginDirMode / pluginFileMode are the permissions for installed plugin directories and files.
	pluginDirMode  = 0o755
	pluginFileMode = 0o644
)

// ErrPluginInstall wraps every install failure (bad URL, download, checksum, unsafe archive, bad
// layout) so callers can match it while the message explains the specific cause.
var ErrPluginInstall = errors.New("plugin install failed")

// Ensure the local backend exposes plugin install/uninstall.
var _ api.PluginInstaller = (*Service)(nil)

// InstallPlugin downloads a Headlamp-format plugin tarball (.tar.gz) from req.URL, optionally verifies
// its SHA-256, safely extracts it (rejecting path traversal, symlinks and decompression bombs), locates
// the plugin (a package.json plus its entry bundle), and installs it under ~/.ksail/plugins/<name>,
// replacing any existing install of the same name. It returns the installed plugin's metadata.
//
// SECURITY: an installed plugin runs UNSANDBOXED in the web UI with the user's cluster credentials. The
// SPA gates this behind explicit consent (surfacing that risk); this method assumes that consent and so
// is registered only on the local loopback backend and behind the read-only guard.
func (s *Service) InstallPlugin(
	ctx context.Context,
	req api.PluginInstallRequest,
) (api.PluginInfo, error) {
	return s.plugins.install(ctx, req)
}

// UninstallPlugin removes an installed plugin directory by id (name). The name must be a single local
// path element (no traversal); a missing plugin is reported as api.ErrNotFound.
func (s *Service) UninstallPlugin(_ context.Context, name string) error {
	return s.plugins.uninstall(name)
}

// install downloads, verifies, extracts and installs a plugin into the store's plugins directory,
// replacing any existing install of the same name.
func (p pluginStore) install(
	ctx context.Context,
	req api.PluginInstallRequest,
) (api.PluginInfo, error) {
	dir, err := p.dir()
	if err != nil {
		return api.PluginInfo{}, err
	}

	staging, root, err := p.stagePlugin(ctx, dir, req)
	if err != nil {
		return api.PluginInfo{}, err
	}

	defer func() { _ = os.RemoveAll(staging) }()

	name, info, err := readPluginManifest(root, req.Name)
	if err != nil {
		return api.PluginInfo{}, err
	}

	err = installStagedPlugin(root, filepath.Join(dir, name))
	if err != nil {
		return api.PluginInfo{}, err
	}

	info.Name = name

	return info, nil
}

// stagePlugin downloads the archive and extracts it into a fresh staging directory under the plugins
// root, returning that staging directory (for the caller to remove) and the located plugin root within
// it. On any failure it removes its own staging directory before returning.
func (p pluginStore) stagePlugin(
	ctx context.Context,
	dir string,
	req api.PluginInstallRequest,
) (string, string, error) {
	archive, err := downloadPluginArchive(ctx, req.URL, req.SHA256)
	if err != nil {
		return "", "", err
	}

	err = os.MkdirAll(dir, pluginDirMode)
	if err != nil {
		return "", "", fmt.Errorf("%w: create plugins dir: %w", ErrPluginInstall, err)
	}

	staging, err := os.MkdirTemp(dir, ".staging-*")
	if err != nil {
		return "", "", fmt.Errorf("%w: staging dir: %w", ErrPluginInstall, err)
	}

	root, err := stageArchive(archive, staging)
	if err != nil {
		_ = os.RemoveAll(staging)

		return "", "", err
	}

	return staging, root, nil
}

// stageArchive extracts the archive into staging and locates the plugin root within it.
func stageArchive(archive []byte, staging string) (string, error) {
	err := extractTarGz(archive, staging)
	if err != nil {
		return "", err
	}

	return locatePluginRoot(staging)
}

// installStagedPlugin replaces any existing install at dest with the staged plugin root (remove +
// rename on the same filesystem, so the swap is close to atomic).
func installStagedPlugin(root, dest string) error {
	err := os.RemoveAll(dest)
	if err != nil {
		return fmt.Errorf("%w: clear existing install: %w", ErrPluginInstall, err)
	}

	err = os.Rename(root, dest)
	if err != nil {
		return fmt.Errorf("%w: install: %w", ErrPluginInstall, err)
	}

	return nil
}

// uninstall removes an installed plugin directory by id.
func (p pluginStore) uninstall(name string) error {
	dir, err := p.dir()
	if err != nil {
		return err
	}

	if !filepath.IsLocal(name) {
		return fmt.Errorf("%w: plugin %q", api.ErrNotFound, name)
	}

	target := filepath.Join(dir, name)

	stat, err := os.Stat(target)
	if err != nil || !stat.IsDir() {
		return fmt.Errorf("%w: plugin %q", api.ErrNotFound, name)
	}

	err = os.RemoveAll(target)
	if err != nil {
		return fmt.Errorf("remove plugin %q: %w", name, err)
	}

	return nil
}

// validatePluginURL parses rawURL and requires it to be an absolute http(s) address.
func validatePluginURL(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("%w: URL must be an http(s) address", ErrPluginInstall)
	}

	// Constrain the download host to known plugin sources. This is the primary defence against a
	// user-supplied URL being turned into a server-side request forgery against the host's internal
	// network or cloud metadata endpoint: the fetch can only target GitHub / Artifact Hub (where
	// Headlamp plugins are published) or a loopback address (local development).
	if !isTrustedPluginHost(parsed.Hostname()) {
		return nil, fmt.Errorf(
			"%w: host %q is not an allowed plugin source (only GitHub, Artifact Hub, or localhost)",
			ErrPluginInstall,
			parsed.Hostname(),
		)
	}

	return parsed, nil
}

// isTrustedPluginHost reports whether host is an allowed plugin-download source: GitHub (where Headlamp
// plugins are released, including its release-asset hosts), Artifact Hub (the catalog source), or a
// loopback host (local development). It is an exact allowlist of string literals — not a suffix or
// IP-range test — both so a look-alike host (e.g. github.com.evil.com) cannot slip through, and so it
// reads as a request-forgery barrier confining the download to a fixed set of known hosts.
func isTrustedPluginHost(host string) bool {
	switch strings.ToLower(host) {
	case "github.com",
		"raw.githubusercontent.com",
		"objects.githubusercontent.com",
		"release-assets.githubusercontent.com",
		"codeload.github.com",
		"artifacthub.io",
		"localhost",
		"127.0.0.1",
		"::1":
		return true
	default:
		return false
	}
}

// pluginHTTPClient returns the HTTP client used for plugin downloads. Its dialer resolves the target and
// refuses to connect to a private, link-local (the cloud metadata endpoint 169.254.169.254), or
// unspecified address — defence-in-depth behind the host allowlist that also covers redirect hops and
// DNS rebinding. It dials the validated IP directly so a rebinding cannot swap a public answer for a
// private one between the check and the connection. Loopback is permitted for local development.
func pluginHTTPClient() *http.Client {
	dialer := &net.Dialer{Timeout: pluginDownloadTimeout}

	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(address)
				if err != nil {
					return nil, fmt.Errorf("%w: %w", ErrPluginInstall, err)
				}

				ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
				if err != nil {
					return nil, fmt.Errorf("%w: resolve %q: %w", ErrPluginInstall, host, err)
				}

				for _, addr := range ips {
					if isBlockedPluginIP(addr.IP) {
						return nil, fmt.Errorf(
							"%w: refusing to connect to non-public address %s",
							ErrPluginInstall,
							addr.IP,
						)
					}
				}

				return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
			},
		},
	}
}

// isBlockedPluginIP reports whether ip is a destination a plugin download must never reach even via a
// redirect or DNS rebinding: a private, link-local (cloud metadata), or unspecified address. Loopback is
// permitted for local development.
func isBlockedPluginIP(ip net.IP) bool {
	return ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsUnspecified()
}

// verifyChecksum checks data against the optional hex SHA-256 (a no-op when wantSHA is empty).
func verifyChecksum(data []byte, wantSHA string) error {
	wantSHA = strings.TrimSpace(wantSHA)
	if wantSHA == "" {
		return nil
	}

	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), wantSHA) {
		return fmt.Errorf("%w: SHA-256 checksum mismatch", ErrPluginInstall)
	}

	return nil
}

// downloadPluginArchive fetches the tarball over http(s) with a size cap and timeout, and verifies the
// optional SHA-256. It returns the raw (still compressed) bytes.
func downloadPluginArchive(ctx context.Context, rawURL, wantSHA string) ([]byte, error) {
	parsed, err := validatePluginURL(rawURL)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, pluginDownloadTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %w", ErrPluginInstall, err)
	}

	response, err := pluginHTTPClient().Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: download: %w", ErrPluginInstall, err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"%w: download returned HTTP %d",
			ErrPluginInstall,
			response.StatusCode,
		)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxPluginDownloadBytes+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read download: %w", ErrPluginInstall, err)
	}

	if len(data) > maxPluginDownloadBytes {
		return nil, fmt.Errorf(
			"%w: archive exceeds %d bytes",
			ErrPluginInstall,
			maxPluginDownloadBytes,
		)
	}

	err = verifyChecksum(data, wantSHA)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// extractTarGz safely extracts a gzip-compressed tar into dest. It rejects path traversal (entries that
// escape dest), skips symlinks/hardlinks/devices (which could escape or do harm), and caps the entry
// count and total extracted size (decompression-bomb defence).
func extractTarGz(archive []byte, dest string) error {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("%w: not a gzip archive: %w", ErrPluginInstall, err)
	}

	defer func() { _ = gzipReader.Close() }()

	reader := tar.NewReader(gzipReader)

	var total int64

	var count int

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("%w: read archive: %w", ErrPluginInstall, err)
		}

		count++
		if count > maxPluginFiles {
			return fmt.Errorf("%w: archive has too many entries", ErrPluginInstall)
		}

		written, err := extractTarEntry(reader, header, dest, total)
		if err != nil {
			return err
		}

		total += written
	}
}

// extractTarEntry writes a single archive entry under dest, enforcing the path-containment and
// size-bomb guards. Non-regular, non-directory entries (symlinks, devices, …) are skipped. It returns
// the number of file bytes written so the caller can track the running total.
func extractTarEntry(
	reader io.Reader,
	header *tar.Header,
	dest string,
	total int64,
) (int64, error) {
	local := filepath.FromSlash(header.Name)
	if !filepath.IsLocal(local) {
		return 0, fmt.Errorf("%w: unsafe path %q in archive", ErrPluginInstall, header.Name)
	}

	target := filepath.Join(dest, local)

	switch header.Typeflag {
	case tar.TypeDir:
		err := os.MkdirAll(target, pluginDirMode)
		if err != nil {
			return 0, fmt.Errorf("%w: create directory: %w", ErrPluginInstall, err)
		}

		return 0, nil
	case tar.TypeReg:
		if header.Size < 0 || total+header.Size > maxPluginExtractedBytes {
			return 0, fmt.Errorf(
				"%w: archive exceeds %d bytes",
				ErrPluginInstall,
				maxPluginExtractedBytes,
			)
		}

		err := os.MkdirAll(filepath.Dir(target), pluginDirMode)
		if err != nil {
			return 0, fmt.Errorf("%w: create directory: %w", ErrPluginInstall, err)
		}

		return writePluginFile(target, reader, header.Size)
	default:
		// Skip symlinks, hardlinks, devices and fifos — they could escape the directory or do harm.
		return 0, nil
	}
}

// writePluginFile copies at most size bytes from reader into target.
func writePluginFile(target string, reader io.Reader, size int64) (int64, error) {
	// target is validated by filepath.IsLocal and rooted at the staging dir, so it cannot escape.
	file, err := os.OpenFile( //nolint:gosec // path validated, rooted at staging
		target,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		pluginFileMode,
	)
	if err != nil {
		return 0, fmt.Errorf("%w: create file: %w", ErrPluginInstall, err)
	}

	defer func() { _ = file.Close() }()

	written, err := io.Copy(file, io.LimitReader(reader, size))
	if err != nil {
		return written, fmt.Errorf("%w: write file: %w", ErrPluginInstall, err)
	}

	return written, nil
}

// locatePluginRoot finds the plugin root in an extracted archive: either the staging directory itself
// (a flat package.json) or its single subdirectory containing one. It errors when neither holds a
// package.json, so an unexpected layout fails loudly rather than installing nothing.
func locatePluginRoot(staging string) (string, error) {
	if fileExists(filepath.Join(staging, pluginPackageFile)) {
		return staging, nil
	}

	entries, err := os.ReadDir(staging)
	if err != nil {
		return "", fmt.Errorf("%w: read archive: %w", ErrPluginInstall, err)
	}

	var dirs []string

	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}

	if len(dirs) == 1 && fileExists(filepath.Join(staging, dirs[0], pluginPackageFile)) {
		return filepath.Join(staging, dirs[0]), nil
	}

	return "", fmt.Errorf(
		"%w: no %s found (expected a flat or single-directory plugin)",
		ErrPluginInstall,
		pluginPackageFile,
	)
}

// readPluginManifest reads the plugin's package.json from root (containment-checked), derives the
// install id (nameOverride if set, else the package name sanitised to a path element), validates the
// entry bundle exists, and returns the id plus the PluginInfo to report.
func readPluginManifest(root, nameOverride string) (string, api.PluginInfo, error) {
	data, err := fsutil.ReadFileSafe(root, filepath.Join(root, pluginPackageFile))
	if err != nil {
		return "", api.PluginInfo{}, fmt.Errorf(
			"%w: read %s: %w",
			ErrPluginInstall,
			pluginPackageFile,
			err,
		)
	}

	var pkg pluginPackage

	_ = json.Unmarshal(data, &pkg)

	info := api.PluginInfo{
		Main:        defaultPluginMain,
		Title:       pkg.Name,
		Version:     pkg.Version,
		Description: pkg.Description,
	}
	if pkg.Main != "" {
		info.Main = pkg.Main
	}

	name := sanitisePluginName(firstNonEmpty(nameOverride, pkg.Name))
	if name == "" {
		return "", api.PluginInfo{}, fmt.Errorf(
			"%w: could not determine a plugin name",
			ErrPluginInstall,
		)
	}

	if !filepath.IsLocal(info.Main) || !fileExists(filepath.Join(root, info.Main)) {
		return "", api.PluginInfo{}, fmt.Errorf(
			"%w: entry bundle %q is missing",
			ErrPluginInstall,
			info.Main,
		)
	}

	return name, info, nil
}

// sanitisePluginName turns a package name (possibly scoped, e.g. "@org/plugin") into a safe single path
// element: it drops a leading "@", flattens "/" to "-", and keeps only [A-Za-z0-9._-]. It returns the
// empty string for a name that reduces to nothing or to "."/"..".
func sanitisePluginName(name string) string {
	name = strings.ReplaceAll(strings.TrimPrefix(strings.TrimSpace(name), "@"), "/", "-")

	var builder strings.Builder

	for _, r := range name {
		if isPluginNameRune(r) {
			builder.WriteRune(r)
		}
	}

	out := builder.String()
	if out == "." || out == ".." {
		return ""
	}

	return out
}

// isPluginNameRune reports whether r is allowed in a sanitised plugin id.
func isPluginNameRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '.', r == '_', r == '-':
		return true
	default:
		return false
	}
}

// firstNonEmpty returns the first non-empty (trimmed) argument, or "".
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

// fileExists reports whether p exists and is a regular file.
func fileExists(p string) bool {
	stat, err := os.Stat(p)

	return err == nil && !stat.IsDir()
}
