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

	// header is the literal first line cloud-init requires to recognise a payload
	// as a cloud-config document; it must be the very first bytes of user_data.
	header = "#cloud-config\n"
	// scriptShebang and scriptSetFlags open the generated boot script. `set -eu`
	// aborts on the first failing command or unset variable, and the `exec`
	// redirect sends every subsequent line's stdout and stderr to the log without
	// masking the script's exit code (unlike a trailing `| tee`).
	scriptShebang  = "#!/bin/sh"
	scriptSetFlags = "set -eu"
)

// Config is the typed input for [BuildUserData]. It captures what to run at first
// boot and where the boot script and its log live; it does not perform any I/O.
type Config struct {
	// Commands are the shell commands run, in order, once at first boot. At least
	// one non-empty command is required (see [ErrNoCommands]); each must be a single
	// line with no NUL byte (see [ErrInvalidCommand]).
	Commands []string
	// ScriptPath overrides where the boot script is written. Optional; defaults to
	// [DefaultScriptPath]. Must be absolute when set (see [ErrPathNotAbsolute]).
	ScriptPath string
	// LogPath overrides where the boot script's combined output is captured.
	// Optional; defaults to [DefaultLogPath]. Must be absolute when set (see
	// [ErrPathNotAbsolute]).
	LogPath string
}

// cloudConfig is the subset of the cloud-init schema this package emits. It is
// marshalled to YAML and prefixed with the `#cloud-config` header.
type cloudConfig struct {
	//nolint:tagliatelle // cloud-init's schema mandates the snake_case key "write_files".
	WriteFiles []writeFile `yaml:"write_files"`
	RunCmd     [][]string  `yaml:"runcmd"`
}

// writeFile is one entry of cloud-init's write_files module: a file dropped onto
// the server before runcmd executes.
type writeFile struct {
	Path        string `yaml:"path"`
	Permissions string `yaml:"permissions"`
	Content     string `yaml:"content"`
}

// BuildUserData renders cfg into a cloud-init user_data document. The returned
// string is a complete `#cloud-config` payload suitable for
// CreateServerOpts.UserData: it writes a boot script containing cfg.Commands and
// runs it once at first boot, capturing all output to the log path.
//
// BuildUserData is pure and never returns a partially-valid document: any
// configuration error (see the package's sentinel errors) is reported instead.
func BuildUserData(cfg Config) (string, error) {
	scriptPath, logPath, err := cfg.resolvePaths()
	if err != nil {
		return "", err
	}

	commands, err := cfg.validatedCommands()
	if err != nil {
		return "", err
	}

	doc := cloudConfig{
		WriteFiles: []writeFile{{
			Path:        scriptPath,
			Permissions: "0700",
			Content:     buildScript(logPath, commands),
		}},
		// argv form (not a shell string) so cloud-init runs the script directly and
		// preserves its exit code; the script itself supplies the shell.
		RunCmd: [][]string{{"/bin/sh", scriptPath}},
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
// configuration with no runnable command or with a command that is not a single
// line. Empty or whitespace-only commands are dropped so a stray "" in the input
// does not emit a blank script line.
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

	if len(commands) == 0 {
		return nil, ErrNoCommands
	}

	return commands, nil
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
