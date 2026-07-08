// Package imageverifier holds the containerd image-verifier-plugin TOML snippet shared by the K3d
// (K3s) and Kind containerd config templates. Both distributions configure the same
// "io.containerd.image-verifier.v1.bindir" plugin the same way — only the node-image wording differs
// ("K3s node image" vs "Kind node image") — so the snippet is generated once here, parameterized by
// the distribution's display name, instead of hand-kept in sync across two files.
package imageverifier

import "fmt"

// bindirPatchFormat is the containerd image-verifier bindir plugin TOML, plus the Cosign/Notation
// example blocks that follow it. %s is substituted with the distribution's display name (e.g. "K3s",
// "Kind") in the two "install the verifier binary" example comments.
const bindirPatchFormat = `[plugins."io.containerd.image-verifier.v1.bindir"]
bin_dir = "/opt/image-verifier/bin"
max_verifiers = 10
per_verifier_timeout = "10s"

# --- Example: Cosign keyless verification (OIDC / Sigstore) ---
# Install the cosign verifier binary into /opt/image-verifier/bin/ in a custom %[1]s node image.
# Cosign will verify signatures using the Sigstore public good instance by default.
# See: https://docs.sigstore.dev/cosign/

# --- Example: Notation verification ---
# Install the notation verifier binary into /opt/image-verifier/bin/ in a custom %[1]s node image.
# Configure trust policies and certificates in the notation config directory.
# See: https://notaryproject.dev/docs/`

// BindirPatch returns the image-verifier bindir plugin TOML snippet (plugin config + Cosign/Notation
// examples), with distribution substituted into the node-image wording. distribution is the
// distribution's display name as it should read in the generated comments, e.g. "K3s" or "Kind".
func BindirPatch(distribution string) string {
	return fmt.Sprintf(bindirPatchFormat, distribution)
}
