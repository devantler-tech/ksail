package hetznerbase_test

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/internal/hetznerbase"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// userDataCarrying wraps a fixture body in a minimal cloud-config document so
// each case exercises the real YAML walk rather than a bare scalar.
// providerCeilingBytes mirrors the package's maxProviderUserDataBytes. Declared
// once here so the boundary cases below cannot drift apart from each other.
const providerCeilingBytes = 32768

func userDataCarrying(body string) string {
	return fmt.Sprintf(`#cloud-config
write_files:
  - path: /etc/kubernetes/ksail/kubeadm-config.yaml
    content: |-
      %s
`, strings.ReplaceAll(body, "\n", "\n      "))
}

// TestDeriveServerSpecsRejectsCertificateTransportMarkers pins the guard against
// the kubeadm certificate-transport class. `--upload-certs` stores the cluster
// PKI in a Secret and the certificate key decrypts it, so either one in
// provider user-data hands the provider the cluster identity even though no PEM
// block appears anywhere.
//
// Every fixture is deliberately free of PEM private-key material, so a failure
// is attributable only to the transport marker and never to the pre-existing
// PEM detection.
func TestDeriveServerSpecsRejectsCertificateTransportMarkers(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"camelCase field":   "certificateKey: abcdef0123456789",
		"kebab-case flag":   "kubeadm join --certificate-key abcdef0123456789",
		"continued flag":    "kubeadm join --certificate-\\\nkey abcdef0123456789",
		"upload-certs flag": "kubeadm init --upload-certs",
		"camelCase option":  "uploadCerts: true",
		// Neither the kebab nor the camel spelling: a literal-list guard misses
		// this, so it pins that detection normalises rather than enumerates.
		"snake_case field": "certificate_key: abcdef0123456789",
		// The separator class is open-ended, so enumerating members of it is the
		// same mistake as enumerating spellings: each of these is a separator no
		// enumerating normaliser strips, and each rewrites the marker into a
		// token the guard would otherwise not recognise.
		"dot-separated field":   "certificate.key: abcdef0123456789",
		"slash-separated field": "certificate/key: abcdef0123456789",
		// Whitespace inside the marker is a token boundary, so these are caught
		// by the assignment shape rather than by normalisation: a field carrying
		// a value is material in provider-readable user-data whatever the field
		// is called, while the same two words in a sentence are not.
		"space-separated field":        "certificate key: abcdef0123456789",
		"space-separated upload-certs": "upload certs = true",
		// Reuses the guard's existing base64 unwrapping, proving the marker
		// class is inspected through the same nesting pipeline as PEM material.
		"base64 nested": base64.StdEncoding.EncodeToString(
			[]byte("certificateKey: abcdef0123456789"),
		),
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			nodes := twoNodeSpecs()
			nodes[1].UserData = userDataCarrying(body)

			specs, err := hetznerbase.DeriveServerSpecs(
				specTestClusterName, nodes, specTestOptions(), specTestInfra(),
			)

			require.ErrorIs(t, err, hetznerbase.ErrSigningTransportInUserData)
			assert.Nil(t, specs)
		})
	}
}

// TestDeriveServerSpecsRejectsClusterPKIKeyPaths pins the on-disk half. kubeadm
// mints the private cluster PKI on the initial control plane, so a path under
// the PKI directory ending in .key appearing in user-data means the renderer is
// writing that material rather than letting kubeadm generate it.
//
// The fixtures carry the path only — no PEM body — so the pre-existing PEM
// detection cannot account for the rejection.
func TestDeriveServerSpecsRejectsClusterPKIKeyPaths(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"cluster CA":                     "cp /tmp/staged /etc/kubernetes/pki/ca.key",
		"front proxy CA":                 "cp /tmp/staged /etc/kubernetes/pki/front-proxy-ca.key",
		"etcd CA":                        "cp /tmp/staged /etc/kubernetes/pki/etcd/ca.key",
		"service account":                "cp /tmp/staged /etc/kubernetes/pki/sa.key",
		"quoted key suffix":              `cp /tmp/staged /etc/kubernetes/pki/ca."key"`,
		"PKI working directory":          "cd /etc/kubernetes/pki && install -m 0600 /tmp/staged ca.key",
		"PKI logical working directory":  "cd -L /etc/kubernetes/pki && install -m 0600 /tmp/staged ca.key",
		"PKI physical working directory": "cd -P /etc/kubernetes/pki && install -m 0600 /tmp/staged ca.key",
		// `cd` persists until the next one, so a key written on a LATER line is
		// still written into the PKI directory. This pins that the fix for the
		// absolute-path false positive did not buy quiet by bounding the rule to a
		// single line -- doing so drops exactly this case, turning a noisy refusal
		// into a silent miss.
		"PKI working directory, key on a later line": "cd /etc/kubernetes/pki && cat ca.crt\n" +
			"install -m 0600 /tmp/staged ca.key",
		"PKI physical error options":   "cd -P -e /etc/kubernetes/pki && install -m 0600 /tmp/staged ca.key",
		"PKI combined physical error":  "cd -Pe /etc/kubernetes/pki && install -m 0600 /tmp/staged ca.key",
		"PKI reordered physical error": "cd -eP /etc/kubernetes/pki && install -m 0600 /tmp/staged ca.key",
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			nodes := twoNodeSpecs()
			nodes[1].UserData = userDataCarrying(body)

			specs, err := hetznerbase.DeriveServerSpecs(
				specTestClusterName, nodes, specTestOptions(), specTestInfra(),
			)

			require.ErrorIs(t, err, hetznerbase.ErrSigningTransportInUserData)
			assert.Nil(t, specs)
		})
	}
}

// TestDeriveServerSpecsAcceptsMaterialFreeUserData is the negative control. A
// guard that rejects everything proves nothing, so each fixture is user-data the
// supported bring-up legitimately produces and must keep accepting.
func TestDeriveServerSpecsAcceptsMaterialFreeUserData(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		// The bootstrap token is provider-visible by design and is not signing
		// material; rejecting it would break the supported join path.
		"bootstrap token": "token: abcdef.0123456789abcdef",
		// The public CA certificate and its discovery hash are public halves.
		"discovery hash": "discoveryTokenCACertHash: sha256:0123456789abcdef",
		// The rendered kubeadm config's own path sits under a different
		// directory and must not trip the PKI-path rule.
		"kubeadm config path": "cat /etc/kubernetes/ksail/kubeadm-config.yaml",
		// A public certificate under the PKI directory is not a private key.
		"public PKI cert": "cat /etc/kubernetes/pki/ca.crt",
		// Changing into the PKI directory is not itself a leak; the relative
		// path rule must still discriminate a public certificate from a key.
		"public PKI cert from working directory": "cd /etc/kubernetes/pki && cat ca.crt",
		// PROSE, not a setting. User-data legitimately carries comments and
		// operator documentation, and a normaliser that drops whitespace along
		// with every other separator joins the words either side of a sentence
		// boundary: "certificate." + "Key" becomes the marker token and the
		// deploy is refused on ordinary content. These pin the token boundary
		// that keeps that from happening; none of them assigns a value, which
		// is what separates them from the space-separated FIELD above.
		"sentence boundary before Key": "# Install the TLS certificate. " +
			"Key material stays in OpenBao.",
		"sentence boundary before Keys": "# Rotate the certificate. " +
			"Keys live in the vault.",
		"plural inside one sentence": "# Renew the certificate keys every 90 days.",
		"upload certs in prose":      "# Upload certs are handled by the operator.",

		// ASSIGNMENT-SHAPED but material-free. The spaced-assignment rule exists
		// to catch a hand-written `certificate key: <hex>`, but user-data
		// legitimately carries shell that PRINTS that shape and comments that
		// document it. Matching on the shape alone refuses a valid bring-up, so
		// the VALUE is what decides: a kubeadm certificate key is a long hex
		// token, and an upload-certs setting that matters is one being switched
		// ON. Prose values and a disabled setting carry nothing.
		"shell echoing a log line":         `echo "certificate key: generated during bootstrap"`,
		"shell echoing a disabled setting": `echo "upload certs: disabled"`,
		"comment documenting the field":    "# certificate key: documented here",

		// A LATER LINE is a different command. The working-directory rule exists
		// to catch `cd /etc/kubernetes/pki && install ... ca.key`, where the key
		// is the argument of the command the `&&` chains. Once the shell moves to
		// the next LINE it is no longer operating in that chained command, so a
		// `.key` there is unrelated -- here an apt keyring written to a wholly
		// different directory. Without a newline bound the rule reads the whole
		// scalar after one `cd`, and refuses a bring-up that leaks nothing.
		"apt keyring on a line after a PKI cd": "cd /etc/kubernetes/pki && cat ca.crt\n" +
			"curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.34/deb/Release.key " +
			"-o /etc/apt/keyrings/k8s.key",
	}

	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			nodes := twoNodeSpecs()
			nodes[1].UserData = userDataCarrying(body)

			specs, err := hetznerbase.DeriveServerSpecs(
				specTestClusterName, nodes, specTestOptions(), specTestInfra(),
			)

			require.NoError(t, err)
			assert.NotEmpty(t, specs)
		})
	}
}

// gzipUserData compresses a cloud-config body the way cloud-init accepts it:
// raw gzip bytes as the whole user-data, with no base64 wrapper.
func gzipUserData(t *testing.T, body string) string {
	t.Helper()

	var buf bytes.Buffer

	writer := gzip.NewWriter(&buf)
	_, err := writer.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	return buf.String()
}

// TestDeriveServerSpecsHandlesRawGzipUserData pins top-level compressed
// user-data, which reaches the guards as gzip bytes rather than YAML.
//
// Before the unwrap, the document scan simply failed to parse those bytes. That
// failed CLOSED rather than open — nothing leaked — but it made the guard reject
// legitimate compressed user-data, and it reported a marker inside compressed
// user-data as a parse error rather than as the transport error that names the
// actual problem. Both halves are pinned here: the control is what would regress
// if the unwrap were removed, and the marker case is what would regress if the
// unwrap were added without re-running the guards over the expanded text.
func TestDeriveServerSpecsHandlesRawGzipUserData(t *testing.T) {
	t.Parallel()

	t.Run("marker inside raw gzip is rejected by its own error", func(t *testing.T) {
		t.Parallel()

		nodes := twoNodeSpecs()
		nodes[1].UserData = gzipUserData(
			t, userDataCarrying("certificateKey: abcdef0123456789"),
		)

		specs, err := hetznerbase.DeriveServerSpecs(
			specTestClusterName, nodes, specTestOptions(), specTestInfra(),
		)

		require.ErrorIs(t, err, hetznerbase.ErrSigningTransportInUserData)
		assert.Nil(t, specs)
	})

	// The material-free control. This is the case the missing unwrap actually
	// broke: valid compressed user-data carrying no signing material at all was
	// refused because the compressed bytes are not YAML.
	t.Run("material-free raw gzip is accepted", func(t *testing.T) {
		t.Parallel()

		nodes := twoNodeSpecs()
		nodes[1].UserData = gzipUserData(
			t, userDataCarrying("runcmd:\n  - echo hello"),
		)

		specs, err := hetznerbase.DeriveServerSpecs(
			specTestClusterName, nodes, specTestOptions(), specTestInfra(),
		)

		require.NoError(t, err)
		require.Len(t, specs, 2)
		assert.Equal(t, userDataCarrying("runcmd:\n  - echo hello"), specs[1].UserData)
	})

	// Announcing gzip and then failing to expand must not be the way past the
	// guard: uninspectable input is refused, not passed through.
	t.Run("truncated gzip is refused rather than passed through", func(t *testing.T) {
		t.Parallel()

		full := gzipUserData(t, userDataCarrying("runcmd:\n  - echo hello"))

		nodes := twoNodeSpecs()
		nodes[1].UserData = full[:len(full)/2]

		specs, err := hetznerbase.DeriveServerSpecs(
			specTestClusterName, nodes, specTestOptions(), specTestInfra(),
		)

		require.ErrorIs(t, err, hetznerbase.ErrUserDataNotInspectable)
		assert.Nil(t, specs)
	})
}

// TestDeriveServerSpecsBoundsForwardedUserData pins the PROVIDER bound on the
// value that is actually sent, which is a different limit from the inspection
// bound and exists for a different reason.
//
// maxDecodedUserDataBytes caps how much text the guards will read, so a marker
// cannot hide past the inspected prefix. It says nothing about what Hetzner will
// accept. Hetzner's ceiling on user_data is 32 KiB -- the same limit the Talos
// autoscaler path already pins as hetznerUserDataLimitBytes, where exceeding it
// makes the API reject the request with "invalid input in field 'user_data'".
//
// Expanding compressed user-data before forwarding it is what makes the two
// bounds diverge: a payload that is comfortably under the provider ceiling while
// compressed can expand well past it, so forwarding the expanded text turns a
// deployable node into an opaque provider rejection. Refusing locally names the
// real problem instead.
func TestDeriveServerSpecsBoundsForwardedUserData(t *testing.T) {
	t.Parallel()

	// Compresses to far under the provider ceiling, expands to far over it, so
	// the two bounds disagree about this exact payload.
	oversized := userDataCarrying(
		"runcmd:\n  - echo hello\n" + strings.Repeat("# padding\n", 6000),
	)

	t.Run("gzip expanding past the provider ceiling is refused", func(t *testing.T) {
		t.Parallel()

		compressed := gzipUserData(t, oversized)
		require.Less(t, len(compressed), providerCeilingBytes,
			"fixture must be under the provider ceiling while compressed, "+
				"or it would not distinguish the two bounds")
		require.Greater(t, len(oversized), providerCeilingBytes,
			"fixture must exceed the provider ceiling once expanded")

		nodes := twoNodeSpecs()
		nodes[1].UserData = compressed

		specs, err := hetznerbase.DeriveServerSpecs(
			specTestClusterName, nodes, specTestOptions(), specTestInfra(),
		)

		require.ErrorIs(t, err, hetznerbase.ErrUserDataTooLargeForProvider)
		assert.Nil(t, specs)
	})

	// The bound governs the forwarded value whatever produced it, so uncompressed
	// user-data over the ceiling is refused on the same terms rather than being
	// handed to the provider to reject.
	t.Run("uncompressed user-data over the ceiling is refused", func(t *testing.T) {
		t.Parallel()

		nodes := twoNodeSpecs()
		nodes[1].UserData = oversized

		specs, err := hetznerbase.DeriveServerSpecs(
			specTestClusterName, nodes, specTestOptions(), specTestInfra(),
		)

		require.ErrorIs(t, err, hetznerbase.ErrUserDataTooLargeForProvider)
		assert.Nil(t, specs)
	})

	// The control: a payload at EXACTLY the ceiling still goes through, so the
	// bound is a ceiling rather than a blanket refusal of large user-data.
	//
	// The exact length is the point. A comfortably-small payload satisfies this
	// assertion for every fixture in the file, so it would not discriminate `>`
	// from `>=` in the runtime check -- an off-by-one that refused a legitimate
	// 32768-byte node would pass such a control unnoticed.
	t.Run("user-data at the ceiling is accepted", func(t *testing.T) {
		t.Parallel()

		atLimit := userDataAtExactly(t, providerCeilingBytes)
		require.Len(t, atLimit, providerCeilingBytes)

		nodes := twoNodeSpecs()
		nodes[1].UserData = atLimit

		specs, err := hetznerbase.DeriveServerSpecs(
			specTestClusterName, nodes, specTestOptions(), specTestInfra(),
		)

		require.NoError(t, err)
		require.Len(t, specs, 2)
	})

	// The paired case one byte over: together with the control above this pins
	// the comparison itself, not merely that some large payload is refused.
	t.Run("user-data one byte over the ceiling is refused", func(t *testing.T) {
		t.Parallel()

		overBySingleByte := userDataAtExactly(t, providerCeilingBytes+1)
		require.Len(t, overBySingleByte, providerCeilingBytes+1)

		nodes := twoNodeSpecs()
		nodes[1].UserData = overBySingleByte

		specs, err := hetznerbase.DeriveServerSpecs(
			specTestClusterName, nodes, specTestOptions(), specTestInfra(),
		)

		require.ErrorIs(t, err, hetznerbase.ErrUserDataTooLargeForProvider)
		assert.Nil(t, specs)
	})
}

// userDataAtExactly returns valid cloud-config user-data of exactly total bytes,
// padded with a trailing YAML comment run so the document still parses. The
// boundary cases above need the length to be exact, not approximate.
func userDataAtExactly(t *testing.T, total int) string {
	t.Helper()

	base := userDataCarrying("runcmd:\n  - echo hello")
	require.LessOrEqual(t, len(base), total, "base fixture already exceeds the target length")

	return base + strings.Repeat("#", total-len(base))
}
