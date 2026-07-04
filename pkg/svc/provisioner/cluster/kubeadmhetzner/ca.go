package kubeadmhetzner

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

const (
	// caCommonName is the subject common name kubeadm gives its cluster CA; the
	// pre-seeded CA mirrors it so an operator inspecting the certificate sees the
	// same identity kubeadm would have minted itself.
	caCommonName = "kubernetes"
	// caKeyBits is the RSA modulus size of the pre-seeded cluster CA. kubeadm's own
	// default CA is RSA-2048, so matching it keeps the pre-seeded CA
	// indistinguishable from a kubeadm-minted one.
	caKeyBits = 2048
	// caValidityYears is how long the cluster CA stays valid. kubeadm mints its CA
	// with a ten-year lifetime, matched here.
	caValidityYears = 10
	// caSerialBits is the bit length of the CA certificate's random serial number.
	caSerialBits = 128
	// caCertHashPrefix is the algorithm prefix of a kubeadm token-discovery hash;
	// kubeadm accepts only SHA-256 pins. It mirrors the kubeadmbootstrap renderer's
	// own prefix so the hash this package computes passes that renderer's
	// validation.
	caCertHashPrefix = "sha256:"
	// caBackdate offsets the CA's NotBefore into the past to tolerate minor clock
	// skew between where the CA is generated and where a node validates it.
	caBackdate = time.Minute
)

// ClusterCA is a self-signed kubeadm cluster certificate authority the multi-node
// Hetzner bring-up pre-seeds onto the cluster-initialising control plane, together
// with the token-discovery hash the joining nodes pin.
//
// # Why pre-seed a CA
//
// kubeadm normally mints the cluster CA itself during `kubeadm init`, so its
// identity — and therefore the `--discovery-token-ca-cert-hash` a joining node
// pins — is not known until the first control plane is already up. The two-phase
// Hetzner bring-up composes every node's cloud-init before any server boots and
// derives the join endpoint only at run time, so it cannot read a kubeadm-minted
// hash back in time to pin it. Pre-seeding a CA the provisioner generated fixes
// the CA identity up front: [GenerateClusterCA] returns the cert and key to write
// into the init node's PKI directory (so kubeadm reuses them instead of minting
// its own) and the discovery hash to thread into the joining nodes'
// JoinConfiguration. This keeps the safe, pinned discovery path working under the
// run-time two-phase model — kubeadmbootstrap requires it, the alternative being
// the insecure unsafeSkipCAVerification mode.
type ClusterCA struct {
	// CertPEM is the CA certificate in PEM form, written to
	// /etc/kubernetes/pki/ca.crt on the cluster-initialising control plane.
	CertPEM []byte
	// KeyPEM is the CA private key in PEM (PKCS#1) form, written to
	// /etc/kubernetes/pki/ca.key on the cluster-initialising control plane so
	// kubeadm can sign the cluster's leaf certificates with it.
	KeyPEM []byte
	// DiscoveryHash is the "sha256:<hex>" hash of the CA's public key, in the form
	// kubeadm's token discovery pins (--discovery-token-ca-cert-hash). It is
	// computed over the certificate's DER SubjectPublicKeyInfo, exactly as
	// kubeadm's own pubkeypin does, so a joining node verifies the served CA
	// against it.
	DiscoveryHash string
}

// GenerateClusterCA generates a fresh self-signed RSA cluster CA and the kubeadm
// token-discovery hash of its public key. The returned material is pre-seeded onto
// the cluster-initialising control plane (cert and key) and pinned by the joining
// nodes (hash), so the two-phase bring-up can fix the CA identity before any node
// boots. It reaches no network and touches no filesystem; its only external
// dependency is the cryptographic random source.
func GenerateClusterCA() (ClusterCA, error) {
	caKey, err := rsa.GenerateKey(rand.Reader, caKeyBits)
	if err != nil {
		return ClusterCA{}, fmt.Errorf("generate kubeadm cluster CA key: %w", err)
	}

	serialCeiling := new(big.Int).Lsh(big.NewInt(1), caSerialBits)

	serialNumber, err := rand.Int(rand.Reader, serialCeiling)
	if err != nil {
		return ClusterCA{}, fmt.Errorf("generate kubeadm cluster CA serial: %w", err)
	}

	now := time.Now()
	certTemplate := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: caCommonName},
		NotBefore:             now.Add(-caBackdate),
		NotAfter:              now.AddDate(caValidityYears, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(
		rand.Reader, &certTemplate, &certTemplate, &caKey.PublicKey, caKey,
	)
	if err != nil {
		return ClusterCA{}, fmt.Errorf("create kubeadm cluster CA certificate: %w", err)
	}

	caCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		// Unreachable in practice: CreateCertificate emits a well-formed certificate
		// ParseCertificate can always read back. Surfaced rather than dropped so a
		// future change that breaks this is not silently ignored.
		return ClusterCA{}, fmt.Errorf("parse kubeadm cluster CA certificate: %w", err)
	}

	// kubeadm pins SHA-256 over the certificate's DER SubjectPublicKeyInfo (its
	// pubkeypin algorithm), so hashing the parsed cert's RawSubjectPublicKeyInfo
	// yields exactly the value a joining node computes and compares.
	spkiDigest := sha256.Sum256(caCert.RawSubjectPublicKeyInfo)

	return ClusterCA{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		KeyPEM: pem.EncodeToMemory(
			&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)},
		),
		DiscoveryHash: caCertHashPrefix + hex.EncodeToString(spkiDigest[:]),
	}, nil
}
