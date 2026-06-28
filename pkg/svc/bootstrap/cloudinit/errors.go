package cloudinitbootstrap

import "errors"

var (
	// ErrNoCommands is returned when a Config carries no non-empty bootstrap
	// command. A user_data document with nothing to run would create a server that
	// silently never bootstraps, so an empty command set is rejected rather than
	// rendered.
	ErrNoCommands = errors.New("cloud-init: at least one bootstrap command is required")

	// ErrInvalidCommand is returned when a bootstrap command contains a newline or
	// a NUL byte. Each command is written as one line of the generated boot script,
	// so an embedded newline would split it into separate (and likely malformed)
	// statements; a NUL byte cannot appear in a shell script at all. Callers that
	// need multiple statements pass them as separate commands.
	ErrInvalidCommand = errors.New(
		"cloud-init: a bootstrap command must be a single line with no NUL byte",
	)

	// ErrPathNotAbsolute is returned when ScriptPath or LogPath is set to a
	// relative path. cloud-init resolves write_files and runcmd paths on the server
	// with no defined working directory, so both must be absolute to be
	// deterministic.
	ErrPathNotAbsolute = errors.New("cloud-init: ScriptPath and LogPath must be absolute paths")
)
