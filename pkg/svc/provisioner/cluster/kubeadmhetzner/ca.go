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
	// generator mirrors it for callers that need kubeadm-compatible test or
	// migration material.
	caCommonName = "kubernetes"
	// frontProxyCACommonName is the subject common name kubeadm gives the CA that
	// signs the API aggregation layer's front-proxy client certificates.
	frontProxyCACommonName = "front-proxy-ca"
	// etcdCACommonName is the subject common name kubeadm gives the CA that signs
	// etcd's serving and peer certificates.
	etcdCACommonName = "etcd-ca"
	// caKeyBits is the RSA modulus size of generated CA material. kubeadm's own
	// default CA is RSA-2048, so matching it keeps the output compatible.
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

// ClusterCA is a self-signed kubeadm-compatible cluster certificate authority
// together with the token-discovery hash joining nodes pin. The active Hetzner
// bring-up does not transport this private material: kubeadm mints its CA on the
// initial control plane and the provisioner derives the same public discovery
// hash from admin.conf after bring-up.
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
// token-discovery hash of its public key. It reaches no network and touches no
// filesystem; its only external dependency is the cryptographic random source.
func GenerateClusterCA() (ClusterCA, error) {
	pair, caCert, err := mintCA(caCommonName)
	if err != nil {
		return ClusterCA{}, err
	}

	return ClusterCA{
		CertPEM:       pair.CertPEM,
		KeyPEM:        pair.KeyPEM,
		DiscoveryHash: discoveryHashFromParsedCertificate(caCert),
	}, nil
}

// discoveryHashFromCertificate returns kubeadm's token-discovery public-key
// pin for a PEM-encoded CA certificate.
func discoveryHashFromCertificate(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errInvalidCAPEM
	}

	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse CA certificate: %w", err)
	}

	return discoveryHashFromParsedCertificate(certificate), nil
}

// discoveryHashFromParsedCertificate mirrors kubeadm's pubkeypin algorithm:
// SHA-256 over the certificate's DER SubjectPublicKeyInfo.
func discoveryHashFromParsedCertificate(certificate *x509.Certificate) string {
	spkiDigest := sha256.Sum256(certificate.RawSubjectPublicKeyInfo)

	return caCertHashPrefix + hex.EncodeToString(spkiDigest[:])
}

// CertKeyPair is a PEM-encoded certificate and its private key.
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

// ClusterPKI is the full set of kubeadm-compatible PKI material shared between
// control planes. It remains available for compatibility and tests; the active
// Hetzner flow deliberately does not place it in provider user-data.
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
// keypair in kubeadm's own encodings: sa.key is a PKCS#1 RSA private key
// (kubeadm's pkiutil.WriteKey → keyutil.MarshalPrivateKeyToPEM emits RSA keys
// as "RSA PRIVATE KEY" blocks) and sa.pub a PKIX public key.
func generateServiceAccountKeys() (ServiceAccountKeys, error) {
	key, err := rsa.GenerateKey(rand.Reader, caKeyBits)
	if err != nil {
		return ServiceAccountKeys{}, fmt.Errorf("generate service-account key: %w", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		// Unreachable in practice: an RSA public key always marshals to PKIX.
		return ServiceAccountKeys{}, fmt.Errorf("marshal service-account public key: %w", err)
	}

	return ServiceAccountKeys{
		KeyPEM: pem.EncodeToMemory(
			&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)},
		),
		PubPEM: pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}),
	}, nil
}
