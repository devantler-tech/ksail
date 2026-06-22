package clusterapi

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	// envPluginSigningPubKey names the env var holding the trusted ed25519 public key (hex or base64)
	// used to verify an optional plugin signature. Unset ⇒ signing disabled (behaviour unchanged). The
	// value is a PUBLIC key, not a credential.
	envPluginSigningPubKey = "KSAIL_PLUGIN_SIGNING_PUBKEY"
)

// ErrPluginInstall wraps every install failure (bad URL, download, checksum, unsafe archive, bad
// layout) so callers can match it while the message explains the specific cause.
var ErrPluginInstall = errors.New("plugin install failed")

// Ensure the local backend exposes plugin install/uninstall.
var _ api.PluginInstaller = (*Service)(nil)

// InstallPlugin downloads a Headlamp-format plugin tarball (.tar.gz) from req.URL, optionally verifies
// its SHA-256 (integrity) and ed25519 signature (authenticity), safely extracts it (rejecting path
// traversal, symlinks and decompression bombs), locates the plugin (a package.json plus its entry
// bundle), and installs it under ~/.ksail/plugins/<name>, replacing any existing install of the same
// name. It returns the installed plugin's metadata.
//
// TRUST MODEL (layered, each optional and additive):
//
//  1. Transport: the tarball is fetched over the request's HTTPS-capable client, so an https:// source
//     authenticates the server and protects the bytes in transit.
//  2. Integrity (optional): when req.SHA256 is set, the downloaded bytes must match that hex digest, so
//     a tampered or truncated download is rejected before extraction.
//  3. Authenticity (optional): when req.Signature is set, the downloaded bytes must verify against the
//     trusted ed25519 public key configured out-of-band in KSAIL_PLUGIN_SIGNING_PUBKEY. A claimed
//     signature with no trusted key configured is rejected (never silently ignored); with no key and no
//     signature, signing is simply disabled and behaviour is unchanged.
//  4. Consent: an installed plugin runs UNSANDBOXED in the web UI with the user's cluster credentials,
//     so the SPA gates the install behind an explicit consent checkbox surfacing that risk.
//
// The ed25519 scheme is deliberately dependency-free (Go stdlib only) to keep the binary, the separate
// desktop/ module, and the release lean. A heavier, more capable authenticity story — sigstore/cosign
// with keyless signing (Fulcio) and a transparency log (Rekor), and/or OCI-distributed signed plugins —
// is the intended future and would supersede this minimal key-pinned check; it is omitted today because
// its dependency tree would significantly bloat all three artifacts.
//
// SECURITY: this method assumes the SPA's consent gate and so is registered only on the local loopback
// backend and behind the read-only guard.
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
	archive, err := downloadPluginArchive(ctx, req.URL, req.SHA256, req.Signature)
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

	return parsed, nil
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

// verifySignature checks data against the optional base64 ed25519 detached signature, using the trusted
// public key in rawPubKey (hex or base64). It is a no-op when no signature is supplied (behaviour
// unchanged). A supplied signature with no trusted key configured is rejected — a claimed signature is
// never silently ignored — as is an unparseable signature/key or a verification failure.
func verifySignature(data []byte, signature, rawPubKey string) error {
	signature = strings.TrimSpace(signature)
	if signature == "" {
		return nil
	}

	if strings.TrimSpace(rawPubKey) == "" {
		return fmt.Errorf(
			"%w: a signature was supplied but no trusted signing key is configured (set %s)",
			ErrPluginInstall,
			envPluginSigningPubKey,
		)
	}

	pubKey, err := parseSigningPubKey(rawPubKey)
	if err != nil {
		return err
	}

	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return fmt.Errorf("%w: signature is not valid base64: %w", ErrPluginInstall, err)
	}

	if !ed25519.Verify(pubKey, data, sig) {
		return fmt.Errorf("%w: signature verification failed", ErrPluginInstall)
	}

	return nil
}

// parseSigningPubKey decodes a trusted ed25519 public key from hex or base64 and validates its length.
func parseSigningPubKey(rawPubKey string) (ed25519.PublicKey, error) {
	rawPubKey = strings.TrimSpace(rawPubKey)

	key, err := decodeKeyBytes(rawPubKey)
	if err != nil {
		return nil, err
	}

	if len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf(
			"%w: signing key must be %d bytes (got %d)",
			ErrPluginInstall,
			ed25519.PublicKeySize,
			len(key),
		)
	}

	return ed25519.PublicKey(key), nil
}

// decodeKeyBytes decodes rawPubKey as hex first, then base64 (standard, then URL-safe), returning the
// first that parses. It errors only when none do.
func decodeKeyBytes(rawPubKey string) ([]byte, error) {
	decoders := []func(string) ([]byte, error){
		hex.DecodeString,
		base64.StdEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
	}

	for _, decode := range decoders {
		key, err := decode(rawPubKey)
		if err == nil {
			return key, nil
		}
	}

	return nil, fmt.Errorf("%w: signing key is not valid hex or base64", ErrPluginInstall)
}

// downloadPluginArchive fetches the tarball over http(s) with a size cap and timeout, then verifies the
// optional SHA-256 (integrity) and ed25519 signature (authenticity). It returns the raw (still
// compressed) bytes.
func downloadPluginArchive(
	ctx context.Context,
	rawURL, wantSHA, signature string,
) ([]byte, error) {
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

	response, err := http.DefaultClient.Do(request)
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

	err = verifySignature(data, signature, os.Getenv(envPluginSigningPubKey))
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
