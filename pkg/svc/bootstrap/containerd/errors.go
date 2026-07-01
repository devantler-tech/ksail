package containerdbootstrap

import "errors"

// ErrInvalidSandboxImage is returned when ContainerdConfig.SandboxImage is set
// but is not a well-formed, single-line image reference — it contains a space,
// a control character (including a newline), or a double quote. containerd reads
// sandbox_image as a quoted TOML string, so a value carrying whitespace, a line
// break, or a quote would produce a malformed config the runtime rejects at
// startup; it is caught here at render time instead.
var ErrInvalidSandboxImage = errors.New(
	"containerd: sandbox image must be a single-line reference without spaces, " +
		"control characters, or quotes",
)
