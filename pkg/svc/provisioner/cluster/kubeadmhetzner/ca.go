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
	// frontProxyCACommonName is the subject common name kubeadm gives the CA that
	// signs the API aggregation layer's front-proxy client certificates.
	frontProxyCACommonName = "front-proxy-ca"
	// etcdCACommonName is the subject common name kubeadm gives the CA that signs
	// etcd's serving and peer certificates.
	etcdCACommonName = "etcd-ca"
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
	pair, caCert, err := mintCA(caCommonName)
	if err != nil {
		return ClusterCA{}, err
	}

	// kubeadm pins SHA-256 over the certificate's DER SubjectPublicKeyInfo (its
	// pubkeypin algorithm), so hashing the parsed cert's RawSubjectPublicKeyInfo
	// yields exactly the value a joining node computes and compares.
	spkiDigest := sha256.Sum256(caCert.RawSubjectPublicKeyInfo)

	return ClusterCA{
		CertPEM:       pair.CertPEM,
		KeyPEM:        pair.KeyPEM,
		DiscoveryHash: caCertHashPrefix + hex.EncodeToString(spkiDigest[:]),
	}, nil
}

// CertKeyPair is a PEM-encoded certificate and its private key — the form the
// auxiliary pre-seeded CAs (front-proxy, etcd) are carried in.
type CertKeyPair struct {
	// CertPEM is the certificate in PEM form.
	CertPEM []byte
	// KeyPEM is the private key in PEM (PKCS#1) form.
	KeyPEM []byte
}

// ServiceAccountKeys is the RSA keypair kube-controller-manager signs service
// account tokens with (kubeadm's sa.key/sa.pub), in kubeadm's own on-disk
// encodings: a PKCS#8 private key and a PKIX public key.
type ServiceAccountKeys struct {
	// KeyPEM is the private key in PEM (PKCS#8) form, written to
	// /etc/kubernetes/pki/sa.key.
	KeyPEM []byte
	// PubPEM is the public key in PEM (PKIX) form, written to
	// /etc/kubernetes/pki/sa.pub.
	PubPEM []byte
}

// ClusterPKI is the full set of PKI material kubeadm shares between control
// planes, pre-generated at compose time so the whole cluster identity — not
// just the cluster CA — is fixed before any node boots. kubeadm reuses every
// piece it finds at its canonical paths instead of minting its own, which is
// what lets a later high-availability increment seed the same material onto
// additional control planes (kubeadm's manual certificate distribution) with
// no `--upload-certs` channel.
type ClusterPKI struct {
	// CA is the cluster CA plus the token-discovery hash the joining nodes pin.
	CA ClusterCA
	// FrontProxyCA signs the API aggregation layer's front-proxy client
	// certificates (front-proxy-ca.crt/.key).
	FrontProxyCA CertKeyPair
	// EtcdCA signs etcd's serving and peer certificates (etcd/ca.crt/.key).
	EtcdCA CertKeyPair
	// ServiceAccount is the service-account token signing keypair (sa.key/sa.pub).
	ServiceAccount ServiceAccountKeys
}

// GenerateClusterPKI generates the full shared kubeadm PKI: the cluster CA
// (with its discovery hash, as [GenerateClusterCA]), the front-proxy CA, the
// etcd CA, and the service-account keypair. Like [GenerateClusterCA] it
// reaches no network and touches no filesystem.
func GenerateClusterPKI() (ClusterPKI, error) {
	clusterCA, err := GenerateClusterCA()
	if err != nil {
		return ClusterPKI{}, err
	}

	frontProxyCA, _, err := mintCA(frontProxyCACommonName)
	if err != nil {
		return ClusterPKI{}, err
	}

	etcdCA, _, err := mintCA(etcdCACommonName)
	if err != nil {
		return ClusterPKI{}, err
	}

	serviceAccount, err := generateServiceAccountKeys()
	if err != nil {
		return ClusterPKI{}, err
	}

	return ClusterPKI{
		CA:             clusterCA,
		FrontProxyCA:   frontProxyCA,
		EtcdCA:         etcdCA,
		ServiceAccount: serviceAccount,
	}, nil
}

// mintCA generates a fresh self-signed RSA signing CA with the given subject
// common name, in the same shape kubeadm mints its own CAs (RSA-2048, ten-year
// lifetime, cert-sign key usage). The parsed certificate is returned alongside
// the PEM pair so a caller can derive material from it (the cluster CA's
// discovery hash) without re-parsing.
func mintCA(commonName string) (CertKeyPair, *x509.Certificate, error) {
	caKey, err := rsa.GenerateKey(rand.Reader, caKeyBits)
	if err != nil {
		return CertKeyPair{}, nil, fmt.Errorf("generate %s CA key: %w", commonName, err)
	}

	serialCeiling := new(big.Int).Lsh(big.NewInt(1), caSerialBits)

	serialNumber, err := rand.Int(rand.Reader, serialCeiling)
	if err != nil {
		return CertKeyPair{}, nil, fmt.Errorf("generate %s CA serial: %w", commonName, err)
	}

	now := time.Now()
	certTemplate := x509.Certificate{
		SerialNumber:          serialNumber,
		Subject:               pkix.Name{CommonName: commonName},
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
		return CertKeyPair{}, nil, fmt.Errorf("create %s CA certificate: %w", commonName, err)
	}

	caCert, err := x509.ParseCertificate(certDER)
	if err != nil {
		// Unreachable in practice: CreateCertificate emits a well-formed certificate
		// ParseCertificate can always read back. Surfaced rather than dropped so a
		// future change that breaks this is not silently ignored.
		return CertKeyPair{}, nil, fmt.Errorf("parse %s CA certificate: %w", commonName, err)
	}

	return CertKeyPair{
		CertPEM: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}),
		KeyPEM: pem.EncodeToMemory(
			&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(caKey)},
		),
	}, caCert, nil
}

// generateServiceAccountKeys generates the service-account token signing
// keypair in kubeadm's own encodings: sa.key is a PKCS#8 private key and
// sa.pub a PKIX public key.
func generateServiceAccountKeys() (ServiceAccountKeys, error) {
	key, err := rsa.GenerateKey(rand.Reader, caKeyBits)
	if err != nil {
		return ServiceAccountKeys{}, fmt.Errorf("generate service-account key: %w", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		// Unreachable in practice: an RSA key always marshals to PKCS#8.
		return ServiceAccountKeys{}, fmt.Errorf("marshal service-account key: %w", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		// Unreachable in practice: an RSA public key always marshals to PKIX.
		return ServiceAccountKeys{}, fmt.Errorf("marshal service-account public key: %w", err)
	}

	return ServiceAccountKeys{
		KeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}),
		PubPEM: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}),
	}, nil
}
