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
		"upload-certs flag": "kubeadm init --upload-certs",
		"camelCase option":  "uploadCerts: true",
		// Neither the kebab nor the camel spelling: a literal-list guard misses
		// this, so it pins that detection normalises rather than enumerates.
		"snake_case field": "certificate_key: abcdef0123456789",
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
		"cluster CA":      "cp /tmp/staged /etc/kubernetes/pki/ca.key",
		"front proxy CA":  "cp /tmp/staged /etc/kubernetes/pki/front-proxy-ca.key",
		"etcd CA":         "cp /tmp/staged /etc/kubernetes/pki/etcd/ca.key",
		"service account": "cp /tmp/staged /etc/kubernetes/pki/sa.key",
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
		assert.NotNil(t, specs)
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

		require.ErrorIs(t, err, hetznerbase.ErrSigningTransportInUserData)
		assert.Nil(t, specs)
	})
}
