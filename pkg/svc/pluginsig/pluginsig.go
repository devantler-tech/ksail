// Package pluginsig verifies KSail web-UI plugin tarballs with cosign/sigstore (the strongest plugin
// install authenticity tier). It supports two modes:
//
//   - Keyless: a sigstore bundle (Fulcio-issued signing certificate + Rekor transparency-log entry +
//     signature over the tarball) is verified against the public-good sigstore trust root, enforcing an
//     expected signing-certificate identity (a SAN/subject pattern plus the OIDC issuer). This is what
//     `cosign verify-blob --bundle ... --certificate-identity ... --certificate-oidc-issuer ...` does.
//   - Key-based: the tarball is verified against a cosign ECDSA public key (PEM), using the signature
//     carried in the bundle. No transparency log or trust root is required.
//
// It is deliberately a SEPARATE package from pkg/cli/clusterapi: sigstore-go pulls in a large dependency
// tree (Fulcio, Rekor, TUF, protobuf-specs), and clusterapi is reused by the standalone desktop/ Go
// module. Keeping the verifier here — wired into clusterapi only at the `ksail open web` command layer
// (clusterapi.Service.UseCosignVerifier) — keeps sigstore-go out of clusterapi and the desktop binary,
// mirroring how the Copilot SDK is confined to pkg/svc/webchat. The ksail CLI binary already links
// sigstore-go transitively (via the kubescape client), so wiring this in adds no new dependency to it.
package pluginsig

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/sigstore/sigstore/pkg/signature"
)

const (
	// maxBundleBytes caps a sigstore bundle fetched from a URL (a bundle is small JSON; this bounds a
	// hostile or runaway response).
	maxBundleBytes = 4 << 20 // 4 MiB
	// bundleFetchTimeout bounds fetching a bundle from a URL.
	bundleFetchTimeout = 30 * time.Second
)

var (
	// ErrCosignVerify is the base error for every cosign verification failure, so callers can match it
	// while the message explains the specific cause.
	ErrCosignVerify = errors.New("cosign verification failed")
	// errNoBundle is returned when neither an inline bundle nor a bundle URL was supplied.
	errNoBundle = errors.New("no sigstore bundle supplied (set cosign.bundle or cosign.bundleUrl)")
	// errNoIdentity is returned for keyless verification when the expected certificate identity is
	// incomplete (both a subject and an issuer are required).
	errNoIdentity = errors.New(
		"keyless verification requires both an expected identity subject and issuer",
	)
	// errNotECDSAKey is returned when a supplied public key is not an ECDSA key (the only key type cosign
	// blob signing uses by default).
	errNotECDSAKey = errors.New("public key is not an ECDSA key")
	// errBundleURLScheme is returned when a bundle URL is not an absolute http(s) address.
	errBundleURLScheme = errors.New("bundle URL must be an http(s) address")
	// errBundleTooLarge is returned when a fetched bundle exceeds maxBundleBytes.
	errBundleTooLarge = errors.New("bundle exceeds size limit")
	// errBundleFetchStatus is returned when fetching a bundle URL responds with a non-200 status; the
	// HTTP status code is wrapped onto it.
	errBundleFetchStatus = errors.New("fetch bundle: unexpected HTTP status")
)

// trustedRootFunc resolves the sigstore trusted root for keyless verification. It is a field on
// Verifier so tests can inject a fixture root (or stub it) instead of fetching the public-good root from
// TUF over the network.
type trustedRootFunc func() (root.TrustedMaterial, error)

// Verifier verifies plugin tarballs against cosign/sigstore material. The zero value is not usable; use
// New. It satisfies the unexported cosignVerifier seam in pkg/cli/clusterapi (structurally), so the
// `ksail open web` command wires an instance in via Service.UseCosignVerifier.
type Verifier struct {
	// httpClient fetches a bundle supplied as a URL. Defaults to a timeout-bounded client; injectable for
	// tests.
	httpClient *http.Client
	// trustedRoot resolves the keyless trust root. Defaults to the public-good root fetched via TUF;
	// injectable for tests so they need no network.
	trustedRoot trustedRootFunc
}

// New returns a Verifier that fetches the public-good sigstore trust root (via TUF, cached under
// ~/.sigstore) for keyless verification and uses a timeout-bounded HTTP client for bundle URLs.
func New() *Verifier {
	return &Verifier{
		httpClient:  &http.Client{Timeout: bundleFetchTimeout},
		trustedRoot: fetchPublicGoodRoot,
	}
}

// fetchPublicGoodRoot fetches the public-good sigstore trusted root from TUF (cached locally after the
// first fetch). It is the default keyless trust root — the same one cosign uses for public-good keyless
// verification.
func fetchPublicGoodRoot() (root.TrustedMaterial, error) {
	trustedRoot, err := root.FetchTrustedRoot()
	if err != nil {
		return nil, fmt.Errorf("fetch sigstore trusted root: %w", err)
	}

	return trustedRoot, nil
}

// VerifyPlugin verifies the tarball bytes against the supplied cosign material. It selects key-based
// verification when a public key is present, otherwise keyless verification against the trust root with
// the expected certificate identity. It returns nil on success or an error wrapping ErrCosignVerify.
//
// The caller (clusterapi) guarantees material is non-empty.
func (v *Verifier) VerifyPlugin(
	ctx context.Context,
	tarball []byte,
	material *api.PluginCosign,
) error {
	signedBundle, err := v.resolveBundle(ctx, material)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCosignVerify, err)
	}

	if strings.TrimSpace(material.PublicKey) != "" {
		return v.verifyKeyBased(tarball, signedBundle, material.PublicKey)
	}

	return v.verifyKeyless(tarball, signedBundle, material)
}

// verifyKeyBased verifies the tarball against a cosign ECDSA public key (PEM). It builds key-only
// trusted material and a key policy, so no transparency log, certificate identity or trust root is
// required — only that the signature in the bundle verifies against the supplied key over the tarball.
func (v *Verifier) verifyKeyBased(
	tarball []byte,
	signedBundle *bundle.Bundle,
	publicKeyPEM string,
) error {
	publicKey, err := parseECDSAPublicKey(publicKeyPEM)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCosignVerify, err)
	}

	trustedMaterial := root.NewTrustedPublicKeyMaterial(
		func(string) (root.TimeConstrainedVerifier, error) {
			loaded, loadErr := signature.LoadECDSAVerifier(publicKey, crypto.SHA256)
			if loadErr != nil {
				return nil, fmt.Errorf("load ECDSA verifier: %w", loadErr)
			}

			return &nonExpiringVerifier{Verifier: loaded}, nil
		},
	)

	// A bare public-key signature carries no observer timestamp, so verify against the current time and
	// require no transparency log; identity is the key itself.
	sev, err := verify.NewVerifier(trustedMaterial, verify.WithCurrentTime())
	if err != nil {
		return fmt.Errorf("%w: build key verifier: %w", ErrCosignVerify, err)
	}

	policy := verify.NewPolicy(verify.WithArtifact(bytes.NewReader(tarball)), verify.WithKey())

	_, err = sev.Verify(signedBundle, policy)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCosignVerify, err)
	}

	return nil
}

// verifyKeyless verifies the tarball against the public-good trust root, requiring a Rekor transparency
// entry and signed-certificate timestamp and enforcing the expected signing-certificate identity (SAN
// pattern + OIDC issuer) — the standard cosign keyless verification.
func (v *Verifier) verifyKeyless(
	tarball []byte,
	signedBundle *bundle.Bundle,
	material *api.PluginCosign,
) error {
	identity, err := buildCertificateIdentity(material)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCosignVerify, err)
	}

	trustedMaterial, err := v.trustedRoot()
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCosignVerify, err)
	}

	sev, err := verify.NewVerifier(
		trustedMaterial,
		verify.WithSignedCertificateTimestamps(1),
		verify.WithTransparencyLog(1),
		verify.WithObserverTimestamps(1),
	)
	if err != nil {
		return fmt.Errorf("%w: build keyless verifier: %w", ErrCosignVerify, err)
	}

	policy := verify.NewPolicy(
		verify.WithArtifact(bytes.NewReader(tarball)),
		verify.WithCertificateIdentity(identity),
	)

	_, err = sev.Verify(signedBundle, policy)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCosignVerify, err)
	}

	return nil
}

// buildCertificateIdentity builds the expected keyless signing-certificate identity from the request:
// the SAN/subject (exact or regex) and the OIDC issuer (exact or regex). Both are required — a keyless
// signature with no enforced identity would accept any Fulcio-issued certificate, defeating the point.
func buildCertificateIdentity(material *api.PluginCosign) (verify.CertificateIdentity, error) {
	subject := strings.TrimSpace(material.IdentitySubject)
	issuer := strings.TrimSpace(material.IdentityIssuer)

	if subject == "" || issuer == "" {
		return verify.CertificateIdentity{}, errNoIdentity
	}

	sanValue, sanRegex := exactOrRegex(subject, material.IdentitySubjectRegex)
	issuerValue, issuerRegex := exactOrRegex(issuer, material.IdentityIssuerRegex)

	identity, err := verify.NewShortCertificateIdentity(
		issuerValue,
		issuerRegex,
		sanValue,
		sanRegex,
	)
	if err != nil {
		return verify.CertificateIdentity{}, fmt.Errorf("build certificate identity: %w", err)
	}

	return identity, nil
}

// exactOrRegex maps a value to the (exact, regex) argument pair sigstore-go's identity matchers expect:
// when asRegex is set the value is the regex (and the exact slot is empty), otherwise it is the exact
// match (and the regex slot is empty).
func exactOrRegex(value string, asRegex bool) (string, string) {
	if asRegex {
		return "", value
	}

	return value, ""
}

// parseECDSAPublicKey decodes a PEM-encoded PKIX public key and asserts it is ECDSA (the key type cosign
// blob signing uses by default).
func parseECDSAPublicKey(publicKeyPEM string) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("%w: public key is not valid PEM", ErrCosignVerify)
	}

	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	ecdsaKey, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return nil, errNotECDSAKey
	}

	return ecdsaKey, nil
}

// resolveBundle obtains the sigstore bundle: from an inline value (raw JSON or base64-encoded JSON) or by
// fetching it from a URL (size-capped). Exactly one source is expected; inline takes precedence.
func (v *Verifier) resolveBundle(
	ctx context.Context,
	material *api.PluginCosign,
) (*bundle.Bundle, error) {
	inline := strings.TrimSpace(material.Bundle)
	if inline != "" {
		return parseBundleJSON(decodeMaybeBase64(inline))
	}

	bundleURL := strings.TrimSpace(material.BundleURL)
	if bundleURL == "" {
		return nil, errNoBundle
	}

	data, err := v.fetchBundle(ctx, bundleURL)
	if err != nil {
		return nil, err
	}

	return parseBundleJSON(data)
}

// parseBundleJSON unmarshals sigstore-bundle JSON into a bundle.Bundle.
func parseBundleJSON(data []byte) (*bundle.Bundle, error) {
	var signedBundle bundle.Bundle

	err := signedBundle.UnmarshalJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse sigstore bundle: %w", err)
	}

	return &signedBundle, nil
}

// decodeMaybeBase64 returns the base64-decoded bytes when value is valid standard base64, otherwise the
// raw bytes (so a caller can paste either raw bundle JSON or a base64 blob). A JSON document starts with
// '{', which is not valid base64, so raw JSON falls through unchanged.
func decodeMaybeBase64(value string) []byte {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err == nil {
		return decoded
	}

	return []byte(value)
}

// fetchBundle downloads a sigstore bundle from an http(s) URL with a size cap and the client's timeout.
func (v *Verifier) fetchBundle(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, errBundleURLScheme
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("build bundle request: %w", err)
	}

	response, err := v.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch bundle: %w", err)
	}

	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w %d", errBundleFetchStatus, response.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(response.Body, maxBundleBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read bundle: %w", err)
	}

	if len(data) > maxBundleBytes {
		return nil, errBundleTooLarge
	}

	return data, nil
}

// nonExpiringVerifier adapts a signature.Verifier into a root.TimeConstrainedVerifier that is always
// valid: a cosign public key has no embedded validity window, so key-based verification does not
// time-constrain the key (the bundle's signature is what is being checked).
type nonExpiringVerifier struct {
	signature.Verifier
}

// ValidAtTime always reports true: a bare public key carries no validity period.
func (*nonExpiringVerifier) ValidAtTime(_ time.Time) bool {
	return true
}
