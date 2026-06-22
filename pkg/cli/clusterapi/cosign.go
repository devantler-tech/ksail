package clusterapi

import (
	"context"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
)

// cosignVerifier verifies a downloaded plugin tarball against cosign/sigstore material (a sigstore
// bundle plus either an expected keyless certificate identity or a cosign public key). It is the seam
// the local Service uses to perform the strongest authenticity tier without importing the heavy
// sigstore-go dependency tree directly: the implementation (pkg/svc/pluginsig) is wired in by the
// `ksail open web` command via UseCosignVerifier, which keeps sigstore-go out of the core clusterapi
// package — and therefore out of the separate desktop/ module that reuses clusterapi (sigstore-go pulls
// in a large dependency tree). A nil verifier means cosign verification is unavailable, so a request
// carrying cosign material is rejected rather than silently downgraded.
//
// VerifyPlugin returns nil when the tarball verifies, or an error (wrapped under ErrPluginInstall by the
// install flow) when it does not. cosign must be non-empty (the caller checks api.PluginCosign.IsEmpty
// first), so the verifier never has to treat the no-material case.
type cosignVerifier interface {
	VerifyPlugin(ctx context.Context, tarball []byte, cosign *api.PluginCosign) error
}

// UseCosignVerifier wires the cosign/sigstore verification backend (the sigstore-go-backed verifier from
// pkg/svc/pluginsig). Until it is called, an install request carrying cosign material is rejected
// (cosign material was supplied but no verifier is configured) rather than silently downgraded to a
// weaker tier — mirroring how a claimed ed25519 signature is rejected without a trusted key. Requests
// without cosign material are unaffected (they use the SHA-256 + ed25519 tiers).
func (s *Service) UseCosignVerifier(verifier cosignVerifier) {
	s.cosign = verifier
	s.plugins.cosign = verifier
}
