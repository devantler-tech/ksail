package pluginsig_test

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/pluginsig"
	"github.com/devantler-tech/ksail/v7/pkg/webui/api"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/sign"
)

// signedTarball signs data with a freshly generated cosign ECDSA keypair (the sigstore default,
// ECDSA P-256 SHA-256) and returns the sigstore-bundle JSON plus the signer's PEM public key. The
// bundle carries only a message signature (no Fulcio cert, no Rekor entry), which is the key-based
// blob-signing shape `cosign sign-blob` produces and which verifies hermetically with no network.
func signedTarball(t *testing.T, data []byte) ([]byte, string) {
	t.Helper()

	keypair, err := sign.NewEphemeralKeypair(nil)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}

	protoBundle, err := sign.Bundle(&sign.PlainData{Data: data}, keypair, sign.BundleOptions{})
	if err != nil {
		t.Fatalf("sign bundle: %v", err)
	}

	signedBundle, err := bundle.NewBundle(protoBundle)
	if err != nil {
		t.Fatalf("wrap bundle: %v", err)
	}

	bundleJSON, err := signedBundle.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}

	publicKeyPEM, err := keypair.GetPublicKeyPem()
	if err != nil {
		t.Fatalf("public key PEM: %v", err)
	}

	return bundleJSON, publicKeyPEM
}

func TestVerifyPluginKeyBasedAcceptsValidSignature(t *testing.T) {
	t.Parallel()

	tarball := []byte("a plausible plugin tarball payload")
	bundleJSON, publicKeyPEM := signedTarball(t, tarball)

	err := pluginsig.New().VerifyPlugin(context.Background(), tarball, &api.PluginCosign{
		Bundle:    string(bundleJSON),
		PublicKey: publicKeyPEM,
	})
	if err != nil {
		t.Fatalf("verify valid signature: %v", err)
	}
}

func TestVerifyPluginKeyBasedAcceptsBase64Bundle(t *testing.T) {
	t.Parallel()

	tarball := []byte("payload for a base64-encoded bundle")
	bundleJSON, publicKeyPEM := signedTarball(t, tarball)

	err := pluginsig.New().VerifyPlugin(context.Background(), tarball, &api.PluginCosign{
		Bundle:    base64.StdEncoding.EncodeToString(bundleJSON),
		PublicKey: publicKeyPEM,
	})
	if err != nil {
		t.Fatalf("verify with base64 bundle: %v", err)
	}
}

func TestVerifyPluginKeyBasedRejectsTamperedTarball(t *testing.T) {
	t.Parallel()

	original := []byte("the original, signed payload")
	bundleJSON, publicKeyPEM := signedTarball(t, original)

	// Verify a DIFFERENT payload against the bundle signed over the original: the signature must not
	// verify because the artifact digest no longer matches.
	err := pluginsig.New().
		VerifyPlugin(context.Background(), []byte("a tampered payload"), &api.PluginCosign{
			Bundle:    string(bundleJSON),
			PublicKey: publicKeyPEM,
		})
	if !errors.Is(err, pluginsig.ErrCosignVerify) {
		t.Fatalf("verify tampered tarball error = %v, want ErrCosignVerify", err)
	}
}

func TestVerifyPluginKeyBasedRejectsWrongKey(t *testing.T) {
	t.Parallel()

	tarball := []byte("payload signed by one key, verified against another")
	bundleJSON, _ := signedTarball(t, tarball)
	// A different keypair's public key — it must not verify the bundle signed by the first key.
	_, otherPublicKeyPEM := signedTarball(t, []byte("unrelated payload"))

	err := pluginsig.New().VerifyPlugin(context.Background(), tarball, &api.PluginCosign{
		Bundle:    string(bundleJSON),
		PublicKey: otherPublicKeyPEM,
	})
	if !errors.Is(err, pluginsig.ErrCosignVerify) {
		t.Fatalf("verify wrong-key signature error = %v, want ErrCosignVerify", err)
	}
}

func TestVerifyPluginRejectsMissingBundle(t *testing.T) {
	t.Parallel()

	// A public key with neither an inline bundle nor a bundle URL: there is nothing to verify.
	err := pluginsig.New().VerifyPlugin(context.Background(), []byte("payload"), &api.PluginCosign{
		PublicKey: "-----BEGIN PUBLIC KEY-----\nMFk=\n-----END PUBLIC KEY-----",
	})
	if !errors.Is(err, pluginsig.ErrCosignVerify) {
		t.Fatalf("verify missing bundle error = %v, want ErrCosignVerify", err)
	}
}

func TestVerifyPluginKeyBasedRejectsMalformedPublicKey(t *testing.T) {
	t.Parallel()

	tarball := []byte("payload with a malformed verification key")
	bundleJSON, _ := signedTarball(t, tarball)

	err := pluginsig.New().VerifyPlugin(context.Background(), tarball, &api.PluginCosign{
		Bundle:    string(bundleJSON),
		PublicKey: "not a pem public key",
	})
	if !errors.Is(err, pluginsig.ErrCosignVerify) {
		t.Fatalf("verify malformed key error = %v, want ErrCosignVerify", err)
	}
}

func TestVerifyPluginKeylessRequiresIdentity(t *testing.T) {
	t.Parallel()

	tarball := []byte("payload for a keyless verification with no identity")
	bundleJSON, _ := signedTarball(t, tarball)

	// Keyless mode (no public key) with no expected identity must be rejected before any trust-root
	// fetch — an unconstrained keyless policy would accept any Fulcio-issued certificate. This also keeps
	// the test hermetic (it never reaches the network TUF fetch).
	err := pluginsig.New().VerifyPlugin(context.Background(), tarball, &api.PluginCosign{
		Bundle: string(bundleJSON),
	})
	if !errors.Is(err, pluginsig.ErrCosignVerify) {
		t.Fatalf("verify keyless without identity error = %v, want ErrCosignVerify", err)
	}
}
