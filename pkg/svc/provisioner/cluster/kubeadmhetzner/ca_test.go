package kubeadmhetzner_test

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"testing"
	"time"

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

	// The CA is long-lived (kubeadm mints its own with a ten-year lifetime); pin
	// the window so an accidental short expiry — which would silently break joins
	// once the CA lapses — is caught here.
	window := cert.NotAfter.Sub(cert.NotBefore)
	assert.Greater(t, window, 9*365*24*time.Hour)
	assert.Less(t, window, 11*365*24*time.Hour)
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

	// Pin the modulus size (kubeadm's own CA is RSA-2048) so a key-size downgrade
	// in ca.go is caught rather than silently weakening the cluster CA.
	assert.Equal(t, 2048, key.N.BitLen())

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

// requireSigningCA asserts pair is a valid self-signed signing CA with the
// given subject common name whose PEM key belongs to its certificate — the
// contract kubeadm requires of every CA it reuses instead of minting.
func requireSigningCA(t *testing.T, pair kubeadmhetzner.CertKeyPair, commonName string) {
	t.Helper()

	cert := parseCACertificate(t, pair.CertPEM)

	assert.True(t, cert.IsCA)
	assert.True(t, cert.BasicConstraintsValid)
	assert.Equal(t, commonName, cert.Subject.CommonName)
	assert.NotZero(t, cert.KeyUsage&x509.KeyUsageCertSign)

	keyBlock, _ := pem.Decode(pair.KeyPEM)
	require.NotNil(t, keyBlock)
	require.Equal(t, "RSA PRIVATE KEY", keyBlock.Type)

	key, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	require.NoError(t, err)

	certPubKey, ok := cert.PublicKey.(*rsa.PublicKey)
	require.True(t, ok)
	assert.True(t, key.PublicKey.Equal(certPubKey))
}

func TestGenerateClusterPKIMintsTheAuxiliaryCAs(t *testing.T) {
	t.Parallel()

	pki, err := kubeadmhetzner.GenerateClusterPKI()
	require.NoError(t, err)

	// The cluster CA keeps the GenerateClusterCA contract (CN + pinned hash).
	assert.Equal(t, "kubernetes", parseCACertificate(t, pki.CA.CertPEM).Subject.CommonName)
	assert.NotEmpty(t, pki.CA.DiscoveryHash)

	// kubeadm reuses front-proxy and etcd CAs only under their own CNs.
	requireSigningCA(t, pki.FrontProxyCA, "front-proxy-ca")
	requireSigningCA(t, pki.EtcdCA, "etcd-ca")

	// Each authority must be its own identity: sharing a key between the CAs
	// would let a certificate minted under one chain validate under another.
	assert.NotEqual(t, pki.CA.KeyPEM, pki.FrontProxyCA.KeyPEM)
	assert.NotEqual(t, pki.CA.KeyPEM, pki.EtcdCA.KeyPEM)
	assert.NotEqual(t, pki.FrontProxyCA.KeyPEM, pki.EtcdCA.KeyPEM)
}

func TestGenerateClusterPKIServiceAccountKeypairMatchesKubeadmEncodings(t *testing.T) {
	t.Parallel()

	pki, err := kubeadmhetzner.GenerateClusterPKI()
	require.NoError(t, err)

	// kubeadm writes RSA sa.key as PKCS#1 ("RSA PRIVATE KEY" — pkiutil.WriteKey
	// → keyutil.MarshalPrivateKeyToPEM) and sa.pub as PKIX; any other encoding
	// would diverge from kubeadm's on-disk format for the pre-seeded pair.
	keyBlock, _ := pem.Decode(pki.ServiceAccount.KeyPEM)
	require.NotNil(t, keyBlock)
	require.Equal(t, "RSA PRIVATE KEY", keyBlock.Type)

	rsaKey, err := x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
	assert.Equal(t, 2048, rsaKey.N.BitLen())

	pubBlock, _ := pem.Decode(pki.ServiceAccount.PubPEM)
	require.NotNil(t, pubBlock)
	require.Equal(t, "PUBLIC KEY", pubBlock.Type)

	parsedPub, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	require.NoError(t, err)

	rsaPub, isRSAPub := parsedPub.(*rsa.PublicKey)
	require.True(t, isRSAPub)

	// sa.pub must be the public half of sa.key, or token verification would
	// reject every token the controller manager signs.
	assert.True(t, rsaKey.PublicKey.Equal(rsaPub))
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
