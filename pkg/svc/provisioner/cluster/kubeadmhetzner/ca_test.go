package kubeadmhetzner_test

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"

	kubeadmbootstrap "github.com/devantler-tech/ksail/v7/pkg/svc/bootstrap/kubeadm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kubeadmhetzner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseCACertificate decodes and parses the PEM CA certificate, failing the test
// on any malformed output so each case can assert against a real *x509.Certificate.
func parseCACertificate(t *testing.T, certPEM []byte) *x509.Certificate {
	t.Helper()

	block, _ := pem.Decode(certPEM)
	require.NotNil(t, block)
	require.Equal(t, "CERTIFICATE", block.Type)

	cert, err := x509.ParseCertificate(block.Bytes)
	require.NoError(t, err)

	return cert
}

func TestGenerateClusterCAReturnsACASigningCertificate(t *testing.T) {
	t.Parallel()

	clusterCA, err := kubeadmhetzner.GenerateClusterCA()
	require.NoError(t, err)

	cert := parseCACertificate(t, clusterCA.CertPEM)

	// kubeadm reuses a pre-seeded CA only if it is a valid signing CA, so the
	// certificate must carry the CA basic constraint and the cert-sign key usage.
	assert.True(t, cert.IsCA)
	assert.True(t, cert.BasicConstraintsValid)
	assert.Equal(t, "kubernetes", cert.Subject.CommonName)
	assert.NotZero(t, cert.KeyUsage&x509.KeyUsageCertSign)
}

func TestGenerateClusterCAKeyBelongsToTheCertificate(t *testing.T) {
	t.Parallel()

	clusterCA, err := kubeadmhetzner.GenerateClusterCA()
	require.NoError(t, err)

	keyBlock, _ := pem.Decode(clusterCA.KeyPEM)
	require.NotNil(t, keyBlock)
	require.Equal(t, "RSA PRIVATE KEY", keyBlock.Type)

	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	require.NoError(t, err)

	certPubKey, ok := parseCACertificate(t, clusterCA.CertPEM).PublicKey.(*rsa.PublicKey)
	require.True(t, ok)

	// The seeded key must be the one that signs from the seeded certificate, or
	// kubeadm could not use the pair to sign the cluster's leaf certificates.
	assert.True(t, key.PublicKey.Equal(certPubKey))
}

func TestGenerateClusterCADiscoveryHashPinsThePublicKey(t *testing.T) {
	t.Parallel()

	clusterCA, err := kubeadmhetzner.GenerateClusterCA()
	require.NoError(t, err)

	cert := parseCACertificate(t, clusterCA.CertPEM)

	// The hash kubeadm pins is SHA-256 over the certificate's DER
	// SubjectPublicKeyInfo (its pubkeypin algorithm); recomputing it here proves
	// the provisioner pins the very key it seeds.
	digest := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	want := "sha256:" + hex.EncodeToString(digest[:])

	assert.Equal(t, want, clusterCA.DiscoveryHash)
}

func TestGenerateClusterCADiscoveryHashIsAcceptedByTheJoinRenderer(t *testing.T) {
	t.Parallel()

	clusterCA, err := kubeadmhetzner.GenerateClusterCA()
	require.NoError(t, err)

	// The computed hash must satisfy the kubeadmbootstrap join renderer, which
	// rejects any pin that is not a full "sha256:<64-hex>" digest — the contract
	// the two-phase bring-up relies on when it threads this hash into the joining
	// nodes' JoinConfiguration.
	_, err = kubeadmbootstrap.Render(kubeadmbootstrap.NodeConfig{
		Role:              kubeadmbootstrap.RoleAgent,
		Token:             "abcdef.0123456789abcdef",
		APIServerEndpoint: "10.0.0.2:6443",
		CACertHashes:      []string{clusterCA.DiscoveryHash},
	})
	require.NoError(t, err)
}

func TestGenerateClusterCAProducesADistinctCAEachTime(t *testing.T) {
	t.Parallel()

	first, err := kubeadmhetzner.GenerateClusterCA()
	require.NoError(t, err)

	second, err := kubeadmhetzner.GenerateClusterCA()
	require.NoError(t, err)

	// Each cluster gets its own CA identity, so two generations never collide.
	assert.NotEqual(t, first.DiscoveryHash, second.DiscoveryHash)
	assert.NotEqual(t, first.CertPEM, second.CertPEM)
	assert.NotEqual(t, first.KeyPEM, second.KeyPEM)
}
