package cloudinitbootstrap

import "errors"

var (
	// ErrNoCommands is returned when a Config carries no directive at all — no
	// command, package, apt source, or file. A user_data document with nothing to
	// do would create a server that silently never bootstraps, so an empty Config
	// is rejected rather than rendered.
	ErrNoCommands = errors.New(
		"cloud-init: a Config must carry at least one command, package, apt source, or file",
	)

	// ErrInvalidCommand is returned when a bootstrap command contains a newline or
	// a NUL byte. Each command is written as one line of the generated boot script,
	// so an embedded newline would split it into separate (and likely malformed)
	// statements; a NUL byte cannot appear in a shell script at all. Callers that
	// need multiple statements pass them as separate commands.
	ErrInvalidCommand = errors.New(
		"cloud-init: a bootstrap command must be a single line with no NUL byte",
	)

	// ErrInvalidPackage is returned when a package name contains a newline or a NUL
	// byte. Each name is one element of cloud-init's packages: list, so it must be a
	// single line.
	ErrInvalidPackage = errors.New(
		"cloud-init: a package name must be a single line with no NUL byte",
	)

	// ErrInvalidSSHAuthorizedKey is returned when an SSH authorized key contains a
	// newline or a NUL byte. Each key is one element of cloud-init's
	// ssh_authorized_keys: list — one authorized_keys line — so it must be a single
	// line.
	ErrInvalidSSHAuthorizedKey = errors.New(
		"cloud-init: an SSH authorized key must be a single line with no NUL byte",
	)

	// ErrInvalidHostKeys is returned when [Config.HostKeys] is set but incomplete
	// (either half blank), the public half is not a single line, or either half
	// contains a NUL byte. Delivering only half an identity would leave the node
	// serving one host key while the client pins another, so both halves are
	// required together.
	ErrInvalidHostKeys = errors.New(
		"cloud-init: host keys need both the PEM private half and a single-line public half",
	)

	// ErrInvalidAptSource is returned when an [AptSource] has a blank or multi-line
	// Name or Source, a Name containing a path separator, a Key containing a NUL
	// byte, or a Name that duplicates another source's. cloud-init keys the sources
	// map by Name and writes the single Source line to a .list file named after it,
	// so Name must be a unique one-line filename and Source must be present.
	ErrInvalidAptSource = errors.New(
		"cloud-init: an apt source needs a unique single-line Name (no path separator) and Source",
	)

	// ErrInvalidFile is returned when a [File] has a non-absolute or multi-line
	// Path, a Content containing a NUL byte, or an explicit Permissions that is not
	// an octal mode. cloud-init resolves a write_files path with no defined working
	// directory (so it must be absolute) and fails the boot on a malformed mode.
	ErrInvalidFile = errors.New(
		"cloud-init: a file needs an absolute single-line Path, NUL-free Content, and an octal mode",
	)

	// ErrPathNotAbsolute is returned when ScriptPath or LogPath is set to a
	// relative path. cloud-init resolves write_files and runcmd paths on the server
	// with no defined working directory, so both must be absolute to be
	// deterministic.
	ErrPathNotAbsolute = errors.New("cloud-init: ScriptPath and LogPath must be absolute paths")
)
