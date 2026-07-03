package cloudinitbootstrap

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	// DefaultScriptPath is where the generated bootstrap script is written on the
	// server when Config.ScriptPath is empty.
	DefaultScriptPath = "/var/lib/ksail/bootstrap.sh"
	// DefaultLogPath is where the bootstrap script's combined output is captured on
	// the server when Config.LogPath is empty.
	DefaultLogPath = "/var/log/ksail-bootstrap.log"
	// DefaultFilePermissions is the mode applied to a [File] whose Permissions is
	// empty. cloud-init requires an explicit mode string, so a sane default keeps
	// callers from having to spell it out for ordinary files.
	DefaultFilePermissions = "0644"

	// header is the literal first line cloud-init requires to recognise a payload
	// as a cloud-config document; it must be the very first bytes of user_data.
	header = "#cloud-config\n"
	// scriptShebang and scriptSetFlags open the generated boot script. `set -eu`
	// aborts on the first failing command or unset variable, and the `exec`
	// redirect sends every subsequent line's stdout and stderr to the log without
	// masking the script's exit code (unlike a trailing `| tee`).
	scriptShebang  = "#!/bin/sh"
	scriptSetFlags = "set -eu"
	// scriptPermissions is the mode of the generated boot script (owner rwx only:
	// it may embed secrets such as a node token, and it is only ever run as root).
	scriptPermissions = "0700"
)

// Config is the typed input for [BuildUserData]. It captures what to install and
// run at first boot and where the boot script and its log live; it does not
// perform any I/O.
//
// A Config is valid when it carries at least one directive — a command, a
// package, an apt source, or a file (see [ErrNoCommands]). The declarative
// fields (Packages, AptSources, Files) are rendered as cloud-init's own modules
// so a native install needs no `curl | sh` at first boot; cloud-init runs them
// in module order (files, then apt configure, then package install) before any
// Command in the boot script.
type Config struct {
	// Commands are the shell commands run, in order, once at first boot, after the
	// declarative fields below have been applied. Optional; each must be a single
	// line with no NUL byte (see [ErrInvalidCommand]). Blank commands are dropped.
	Commands []string
	// Packages are apt packages installed via cloud-init's `packages:` module
	// before Commands run. Optional; each must be a single line with no NUL byte
	// (see [ErrInvalidPackage]). Blank entries are dropped.
	Packages []string
	// AptSources are additional APT repositories configured (with their signing
	// keys, declaratively) before packages install. Optional; see [AptSource] and
	// [ErrInvalidAptSource].
	AptSources []AptSource
	// Files are files written to the server (cloud-init `write_files:`) before
	// apt/package processing and the boot script. Optional; see [File] and
	// [ErrInvalidFile].
	Files []File
	// SSHAuthorizedKeys are public keys added to the default user's
	// authorized_keys (cloud-init's `ssh_authorized_keys:` module; on Hetzner
	// stock images the default user is root) so a post-provision client — the
	// SSH bootstrap seam (#5696) that retrieves the generated kubeconfig — can
	// authenticate. Optional; each must be a single line with no NUL byte (see
	// [ErrInvalidSSHAuthorizedKey]). Blank entries are dropped. Keys alone do
	// not make a Config renderable: a document with a key but nothing to run
	// would create a server that never bootstraps, so a keys-only Config is
	// still rejected with [ErrNoCommands].
	SSHAuthorizedKeys []string
	// HostKeys is a pre-generated SSH host identity delivered via cloud-init's
	// `ssh_keys:` module, replacing the host keys the image would otherwise
	// generate at first boot. Delivering the identity up front lets the SSH
	// bootstrap client pin the host key (golang.org/x/crypto/ssh.FixedHostKey)
	// instead of trusting first contact. Optional; when set, both halves are
	// required (see [ErrInvalidHostKeys]). Like SSHAuthorizedKeys, a host
	// identity alone does not make a Config renderable ([ErrNoCommands]).
	HostKeys *HostKeys
	// ScriptPath overrides where the boot script is written. Optional; defaults to
	// [DefaultScriptPath]. Must be absolute when set (see [ErrPathNotAbsolute]).
	ScriptPath string
	// LogPath overrides where the boot script's combined output is captured.
	// Optional; defaults to [DefaultLogPath]. Must be absolute when set (see
	// [ErrPathNotAbsolute]).
	LogPath string
}

// AptSource is one entry of cloud-init's `apt: sources:` module: an additional
// APT repository configured before packages install, with its signing key
// trusted declaratively so no key is fetched with `curl | gpg` at runtime.
type AptSource struct {
	// Name is the cloud-init sources-map key; cloud-init writes the source line to
	// /etc/apt/sources.list.d/<Name>.list. Required; a single line with no NUL
	// byte or path separator (it is a filename, not a path), and unique across a
	// Config's AptSources (see [ErrInvalidAptSource]).
	Name string
	// Source is the one-line APT source entry, e.g.
	// "deb [signed-by=/etc/apt/keyrings/k8s.gpg] https://pkgs.k8s.io/core:/stable:/v1.31/deb/ /".
	// Required; a single line with no NUL byte.
	Source string
	// Key is the ASCII-armored public key cloud-init trusts for this source (the
	// `key:` field). Optional; embedded verbatim so the key is trusted
	// declaratively at first boot rather than fetched at runtime. May span
	// multiple lines but must contain no NUL byte.
	Key string
}

// File is one cloud-init `write_files:` entry: a file dropped onto the server
// before apt/package processing and the boot script — e.g. a kubeadm config or
// an apt keyring.
type File struct {
	// Path is the absolute destination path (see [ErrInvalidFile]). Required.
	Path string
	// Permissions is the octal mode string cloud-init applies, e.g. "0600" (three
	// or four octal digits). Optional; defaults to [DefaultFilePermissions]. An
	// explicit non-octal value is rejected (see [ErrInvalidFile]).
	Permissions string
	// Content is the file's contents, written verbatim. May be empty or span
	// multiple lines; only a NUL byte is rejected.
	Content string
}

// HostKeys is cloud-init's `ssh_keys:` module limited to the ed25519 host
// identity this package delivers: the private half the node's sshd serves and
// the matching public half a bootstrap client pins. ed25519 matches the
// bootstrap keypair the ssh bootstrap package generates.
type HostKeys struct {
	// ED25519Private is the PEM-encoded ed25519 private host key. Required when
	// HostKeys is set; spans multiple lines, no NUL byte (see
	// [ErrInvalidHostKeys]).
	ED25519Private string
	// ED25519Public is the single-line (authorized_keys-style) public host key.
	// Required when HostKeys is set (see [ErrInvalidHostKeys]).
	ED25519Public string
}

// cloudConfig is the subset of the cloud-init schema this package emits. It is
// marshalled to YAML and prefixed with the `#cloud-config` header. Every field
// is omitempty so a command-only Config renders exactly as it did before the
// declarative fields existed.
type cloudConfig struct {
	//nolint:tagliatelle // cloud-init's schema mandates the snake_case key "write_files".
	WriteFiles []writeFile `yaml:"write_files,omitempty"`
	Apt        *aptConfig  `yaml:"apt,omitempty"`
	Packages   []string    `yaml:"packages,omitempty"`
	//nolint:tagliatelle // cloud-init's schema mandates the snake_case key "ssh_authorized_keys".
	SSHAuthorizedKeys []string `yaml:"ssh_authorized_keys,omitempty"`
	//nolint:tagliatelle // cloud-init's schema mandates the snake_case key "ssh_keys".
	SSHKeys *sshKeysConfig `yaml:"ssh_keys,omitempty"`
	RunCmd  [][]string     `yaml:"runcmd,omitempty"`
}

// sshKeysConfig is cloud-init's `ssh_keys:` module, limited to the ed25519
// host identity this package emits (see [HostKeys]).
type sshKeysConfig struct {
	//nolint:tagliatelle // cloud-init's schema mandates the snake_case key "ed25519_private".
	ED25519Private string `yaml:"ed25519_private"`
	//nolint:tagliatelle // cloud-init's schema mandates the snake_case key "ed25519_public".
	ED25519Public string `yaml:"ed25519_public"`
}

// writeFile is one entry of cloud-init's write_files module: a file dropped onto
// the server before runcmd executes.
type writeFile struct {
	Path        string `yaml:"path"`
	Permissions string `yaml:"permissions"`
	Content     string `yaml:"content"`
}

// aptConfig is cloud-init's `apt:` module, limited to the `sources:` map this
// package emits. The map is keyed by [AptSource.Name]; yaml.v3 marshals map keys
// in sorted order, so the rendered document is deterministic.
type aptConfig struct {
	Sources map[string]aptSource `yaml:"sources"`
}

// aptSource is one value of the apt sources map.
type aptSource struct {
	Source string `yaml:"source"`
	Key    string `yaml:"key,omitempty"`
}

// BuildUserData renders cfg into a cloud-init user_data document. The returned
// string is a complete `#cloud-config` payload suitable for
// CreateServerOpts.UserData: it drops cfg.Files, configures cfg.AptSources,
// installs cfg.Packages, then writes a boot script containing cfg.Commands and
// runs it once at first boot, capturing that script's output to the log path.
//
// BuildUserData is pure and never returns a partially-valid document: any
// configuration error (see the package's sentinel errors) is reported instead.
func BuildUserData(cfg Config) (string, error) {
	dirs, err := cfg.validate()
	if err != nil {
		return "", err
	}

	doc, err := cfg.buildDoc(dirs)
	if err != nil {
		return "", err
	}

	out, err := yaml.Marshal(doc)
	if err != nil {
		// Unreachable in practice: cloudConfig is a fixed struct of strings, which
		// yaml.Marshal cannot fail to encode. Surfaced rather than dropped so a
		// future schema change that breaks this is not silently ignored.
		return "", fmt.Errorf("marshalling cloud-config: %w", err)
	}

	return header + string(out), nil
}

// directives are cfg's validated inputs, ready to render.
type directives struct {
	commands []string
	packages []string
	sources  map[string]aptSource
	files    []writeFile
	sshKeys  []string
	hostKeys *sshKeysConfig
}

// empty reports whether there is nothing to render — no command, package, apt
// source, or file. SSH authorized keys and the host identity are deliberately
// excluded: a keys-only document would create a server that never bootstraps,
// so keys alone don't make a Config renderable (see
// [Config.SSHAuthorizedKeys]).
func (d directives) empty() bool {
	return len(d.commands) == 0 &&
		len(d.packages) == 0 &&
		len(d.sources) == 0 &&
		len(d.files) == 0
}

// validate returns cfg's validated directives, or the first configuration error
// encountered. A Config with no directive at all is rejected with [ErrNoCommands].
func (cfg Config) validate() (directives, error) {
	commands, err := cfg.validatedCommands()
	if err != nil {
		return directives{}, err
	}

	packages, err := cfg.validatedPackages()
	if err != nil {
		return directives{}, err
	}

	sources, err := cfg.validatedAptSources()
	if err != nil {
		return directives{}, err
	}

	files, err := cfg.validatedFiles()
	if err != nil {
		return directives{}, err
	}

	sshKeys, err := cfg.validatedSSHAuthorizedKeys()
	if err != nil {
		return directives{}, err
	}

	var hostKeys *sshKeysConfig

	if cfg.HostKeys != nil {
		hostKeys, err = cfg.validatedHostKeys()
		if err != nil {
			return directives{}, err
		}
	}

	dirs := directives{
		commands: commands,
		packages: packages,
		sources:  sources,
		files:    files,
		sshKeys:  sshKeys,
		hostKeys: hostKeys,
	}
	if dirs.empty() {
		return directives{}, ErrNoCommands
	}

	return dirs, nil
}

// validatedSSHAuthorizedKeys returns the non-blank SSH public keys of cfg in
// order, rejecting a key that is not a single line. Each key is one element of
// cloud-init's ssh_authorized_keys: list (one authorized_keys line), so it must
// be a single line. Blank entries are dropped.
func (cfg Config) validatedSSHAuthorizedKeys() ([]string, error) {
	keys := make([]string, 0, len(cfg.SSHAuthorizedKeys))

	for _, key := range cfg.SSHAuthorizedKeys {
		if strings.ContainsAny(key, "\n\x00") {
			return nil, ErrInvalidSSHAuthorizedKey
		}

		if strings.TrimSpace(key) == "" {
			continue
		}

		keys = append(keys, key)
	}

	return keys, nil
}

// validatedHostKeys returns cfg's (non-nil) host identity as the rendered
// ssh_keys module. Both halves are required together; the public half must be
// a single line; neither may contain a NUL byte.
func (cfg Config) validatedHostKeys() (*sshKeysConfig, error) {
	private := cfg.HostKeys.ED25519Private
	public := cfg.HostKeys.ED25519Public

	if strings.TrimSpace(private) == "" || strings.TrimSpace(public) == "" {
		return nil, ErrInvalidHostKeys
	}

	if strings.ContainsRune(private, '\x00') || strings.ContainsAny(public, "\n\x00") {
		return nil, ErrInvalidHostKeys
	}

	return &sshKeysConfig{ED25519Private: private, ED25519Public: public}, nil
}

// buildDoc assembles the cloud-config from already-validated directives. Files
// are written first, then apt sources and packages, then (when there are
// commands) the boot script and the runcmd that executes it — matching
// cloud-init's own module order so a package a command relies on is present
// before the command runs.
func (cfg Config) buildDoc(dirs directives) (cloudConfig, error) {
	doc := cloudConfig{
		WriteFiles:        dirs.files,
		Packages:          dirs.packages,
		SSHAuthorizedKeys: dirs.sshKeys,
		SSHKeys:           dirs.hostKeys,
	}

	if len(dirs.sources) > 0 {
		doc.Apt = &aptConfig{Sources: dirs.sources}
	}

	if len(dirs.commands) > 0 {
		scriptPath, logPath, err := cfg.resolvePaths()
		if err != nil {
			return cloudConfig{}, err
		}

		doc.WriteFiles = append(doc.WriteFiles, writeFile{
			Path:        scriptPath,
			Permissions: scriptPermissions,
			Content:     buildScript(logPath, dirs.commands),
		})
		// argv form (not a shell string) so cloud-init runs the script directly and
		// preserves its exit code; the script itself supplies the shell.
		doc.RunCmd = [][]string{{"/bin/sh", scriptPath}}
	}

	return doc, nil
}

// resolvePaths returns the effective script and log paths, applying the defaults
// for empty fields and rejecting any non-absolute override.
func (cfg Config) resolvePaths() (string, string, error) {
	scriptPath := cfg.ScriptPath
	if scriptPath == "" {
		scriptPath = DefaultScriptPath
	}

	logPath := cfg.LogPath
	if logPath == "" {
		logPath = DefaultLogPath
	}

	if !strings.HasPrefix(scriptPath, "/") || !strings.HasPrefix(logPath, "/") {
		return "", "", ErrPathNotAbsolute
	}

	return scriptPath, logPath, nil
}

// validatedCommands returns the non-empty commands of cfg in order, rejecting a
// command that is not a single line. Empty or whitespace-only commands are
// dropped so a stray "" in the input does not emit a blank script line. Unlike
// the pre-declarative behaviour, an empty result is not an error here: the
// caller checks emptiness across all directives.
func (cfg Config) validatedCommands() ([]string, error) {
	commands := make([]string, 0, len(cfg.Commands))

	for _, command := range cfg.Commands {
		if strings.ContainsAny(command, "\n\x00") {
			return nil, ErrInvalidCommand
		}

		if strings.TrimSpace(command) == "" {
			continue
		}

		commands = append(commands, command)
	}

	return commands, nil
}

// validatedPackages returns the non-blank package names of cfg in order,
// rejecting a name that is not a single line. Blank entries are dropped.
func (cfg Config) validatedPackages() ([]string, error) {
	packages := make([]string, 0, len(cfg.Packages))

	for _, pkg := range cfg.Packages {
		if strings.ContainsAny(pkg, "\n\x00") {
			return nil, ErrInvalidPackage
		}

		if strings.TrimSpace(pkg) == "" {
			continue
		}

		packages = append(packages, pkg)
	}

	return packages, nil
}

// validatedAptSources returns cfg.AptSources as a sources map keyed by Name,
// rejecting an entry with a blank or multi-line Name or Source, a Key with a NUL
// byte, or a duplicate Name. An empty (non-nil) map is returned when there are no
// sources.
func (cfg Config) validatedAptSources() (map[string]aptSource, error) {
	sources := make(map[string]aptSource, len(cfg.AptSources))

	for _, src := range cfg.AptSources {
		// Name becomes the /etc/apt/sources.list.d/<Name>.list filename, so a path
		// separator would let it escape that directory — reject it, not just blanks.
		if isBlankOrMultiline(src.Name) ||
			strings.ContainsAny(src.Name, `/\`) ||
			isBlankOrMultiline(src.Source) {
			return nil, ErrInvalidAptSource
		}

		if strings.ContainsRune(src.Key, '\x00') {
			return nil, ErrInvalidAptSource
		}

		if _, exists := sources[src.Name]; exists {
			return nil, ErrInvalidAptSource
		}

		sources[src.Name] = aptSource{Source: src.Source, Key: src.Key}
	}

	return sources, nil
}

// validatedFiles returns cfg.Files as write_files entries, rejecting a file with
// a non-absolute or multi-line Path, a Content with a NUL byte, or an explicit
// Permissions that is not an octal mode, and applying [DefaultFilePermissions]
// to an entry with no explicit mode.
func (cfg Config) validatedFiles() ([]writeFile, error) {
	files := make([]writeFile, 0, len(cfg.Files))

	for _, file := range cfg.Files {
		if strings.ContainsAny(file.Path, "\n\x00") || !strings.HasPrefix(file.Path, "/") {
			return nil, ErrInvalidFile
		}

		if strings.ContainsRune(file.Content, '\x00') {
			return nil, ErrInvalidFile
		}

		// An explicit mode is passed verbatim to cloud-init, which fails the boot if
		// it is not a valid octal string — reject a malformed one at build time.
		if file.Permissions != "" && !isOctalMode(file.Permissions) {
			return nil, ErrInvalidFile
		}

		permissions := file.Permissions
		if permissions == "" {
			permissions = DefaultFilePermissions
		}

		files = append(files, writeFile{
			Path:        file.Path,
			Permissions: permissions,
			Content:     file.Content,
		})
	}

	return files, nil
}

// isBlankOrMultiline reports whether s is empty/whitespace-only or contains a
// newline or NUL byte — the rejection shared by an apt source's Name and Source.
func isBlankOrMultiline(s string) bool {
	return strings.TrimSpace(s) == "" || strings.ContainsAny(s, "\n\x00")
}

// isOctalMode reports whether s is a cloud-init file mode: three or four octal
// digits (e.g. "644" or "0600"), which is what write_files accepts.
func isOctalMode(s string) bool {
	if len(s) != 3 && len(s) != 4 {
		return false
	}

	for _, r := range s {
		if r < '0' || r > '7' {
			return false
		}
	}

	return true
}

// buildScript assembles the POSIX boot script: a shebang, strict-mode flags, an
// exec redirect that captures all output to logPath, then each command on its
// own line. logPath is shell-quoted because it is the one config value spliced
// into a shell context: an absolute-path check alone would still let a path with
// a space break the redirect, or a crafted `/var/log/x; cmd` change the script's
// behaviour. (scriptPath needs no quoting — it only ever appears as a YAML
// scalar and a runcmd argv element, never inside a shell string.)
func buildScript(logPath string, commands []string) string {
	lines := make([]string, 0, len(commands)+3) //nolint:mnd // shebang + flags + exec redirect
	lines = append(lines,
		scriptShebang,
		scriptSetFlags,
		"exec >> "+shellQuote(logPath)+" 2>&1",
	)
	lines = append(lines, commands...)

	return strings.Join(lines, "\n") + "\n"
}

// shellQuote single-quotes s for safe inclusion in a POSIX shell command,
// escaping any embedded single quote via the '\” idiom, so shell
// metacharacters in the value are never interpreted.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
