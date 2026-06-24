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

// InstallPlugin downloads a Headlamp-format plugin tarball (.tar.gz) from req.URL, verifies it against
// whichever authenticity material the request carries, safely extracts it (rejecting path traversal,
// symlinks and decompression bombs), locates the plugin (a package.json plus its entry bundle), and
// installs it under ~/.ksail/plugins/<name>, replacing any existing install of the same name. It
// returns the installed plugin's metadata.
//
// TRUST MODEL (layered, strongest first; each tier is optional and additive):
//
//  1. Transport: the tarball is fetched over the request's HTTPS-capable client, so an https:// source
//     authenticates the server and protects the bytes in transit.
//  2. Cosign / sigstore (STRONGEST authenticity, optional): when req.Cosign is non-empty, the downloaded
//     bytes are verified with sigstore-go — keyless (a sigstore bundle: Fulcio cert + Rekor transparency
//     entry + signature, verified against the public-good trust root and an expected certificate
//     identity: SAN/subject pattern + OIDC issuer) or key-based (against a cosign ECDSA public key). A
//     verification failure rejects the install; cosign material with no verifier wired is rejected too
//     (never silently downgraded). This is the cryptographically strongest tier — the ed25519 and
//     SHA-256 tiers below are lighter fallbacks.
//  3. Integrity (optional): when req.SHA256 is set, the downloaded bytes must match that hex digest, so
//     a tampered or truncated download is rejected before extraction.
//  4. Authenticity, ed25519 (optional, lighter than cosign): when req.Signature is set, the downloaded
//     bytes must verify against the trusted ed25519 public key configured out-of-band in
//     KSAIL_PLUGIN_SIGNING_PUBKEY. A claimed signature with no trusted key configured is rejected (never
//     silently ignored); with no key and no signature, ed25519 signing is simply disabled.
//  5. Consent: an installed plugin runs UNSANDBOXED in the web UI with the user's cluster credentials,
//     so the SPA gates the install behind an explicit consent checkbox surfacing that risk.
//
// The cosign verifier lives behind a seam (cosignVerifier) wired in at the `ksail open web` command
// layer (UseCosignVerifier), so the heavy sigstore-go dependency tree stays out of this package and the
// separate desktop/ module that reuses it. The ed25519 and SHA-256 tiers remain dependency-free (Go
// stdlib only) so they work on every backend, cosign-wired or not.
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
	archive, err := downloadPluginArchive(ctx, req, p.cosign)
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

// downloadPluginArchive fetches the tarball over http(s) with a size cap and timeout, then verifies it
// against whatever authenticity material the request carries: cosign/sigstore (strongest, via the wired
// verifier), the SHA-256 digest (integrity), and the ed25519 signature (lighter authenticity). It
// returns the raw (still compressed) bytes.
func downloadPluginArchive(
	ctx context.Context,
	req api.PluginInstallRequest,
	cosign cosignVerifier,
) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, pluginDownloadTimeout)
	defer cancel()

	data, err := fetchPluginBytes(ctx, req.URL)
	if err != nil {
		return nil, err
	}

	err = verifyPluginArchive(ctx, data, req, cosign)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// fetchPluginBytes downloads the tarball from rawURL (validated as http(s)) with a size cap, returning
// the raw bytes. The caller bounds the timeout via ctx.
func fetchPluginBytes(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := validatePluginURL(rawURL)
	if err != nil {
		return nil, err
	}

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

	return data, nil
}

// verifyPluginArchive runs the authenticity tiers over the downloaded bytes, strongest first: cosign,
// then the SHA-256 digest, then the ed25519 signature. The first failing tier returns its error.
func verifyPluginArchive(
	ctx context.Context,
	data []byte,
	req api.PluginInstallRequest,
	cosign cosignVerifier,
) error {
	err := verifyCosign(ctx, data, req.Cosign, cosign)
	if err != nil {
		return err
	}

	err = verifyChecksum(data, req.SHA256)
	if err != nil {
		return err
	}

	return verifySignature(data, req.Signature, os.Getenv(envPluginSigningPubKey))
}

// verifyCosign runs the strongest authenticity tier: when the request carries cosign material, the
// download must verify against it via the wired verifier. Cosign material with no verifier wired is
// rejected (never silently downgraded to a weaker tier), mirroring how a claimed ed25519 signature is
// rejected without a trusted key. No cosign material is a no-op, so the lighter tiers apply.
func verifyCosign(
	ctx context.Context,
	data []byte,
	material *api.PluginCosign,
	verifier cosignVerifier,
) error {
	if material.IsEmpty() {
		return nil
	}

	if verifier == nil {
		return fmt.Errorf(
			"%w: cosign verification material was supplied but cosign verification is not configured",
			ErrPluginInstall,
		)
	}

	err := verifier.VerifyPlugin(ctx, data, material)
	if err != nil {
		return fmt.Errorf("%w: cosign verification failed: %w", ErrPluginInstall, err)
	}

	return nil
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
