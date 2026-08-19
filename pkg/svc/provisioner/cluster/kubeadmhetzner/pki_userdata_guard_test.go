package kubeadmhetzner_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// pemPrivateKeyMarker is the trailing half of every PEM private-key label.
// PKCS#1 ("RSA PRIVATE KEY"), PKCS#8 ("PRIVATE KEY"), SEC 1 ("EC PRIVATE KEY"),
// DSA, encrypted, and OpenSSH blocks all end in it, so matching the suffix
// covers every encoding a key could arrive in rather than one chosen spelling.
const pemPrivateKeyMarker = "PRIVATE KEY"

// kubeadmCertificateTransportMarkers are the kubeadm settings that move control
// plane signing material between nodes. `--upload-certs` stores the cluster PKI
// in a cluster Secret and `certificateKey` is the symmetric key that decrypts
// it, so either one in provider user-data hands the provider the cluster
// identity even though no PEM block appears.
var kubeadmCertificateTransportMarkers = []string{
	"certificateKey",
	"certificate-key",
	"upload-certs",
	"uploadCerts",
}

// pkiPathMarkers are the on-disk locations of the private halves of the cluster
// PKI. kubeadm mints these on the initial control plane; a path appearing in
// user-data means the renderer is writing the material rather than letting
// kubeadm generate it.
var pkiPathMarkers = []string{
	"/etc/kubernetes/pki/ca.key",
	"/etc/kubernetes/pki/front-proxy-ca.key",
	"/etc/kubernetes/pki/etcd/ca.key",
	"/etc/kubernetes/pki/sa.key",
}

// signingMaterialMarkersIn reports every marker of cluster-signing material
// present in the rendered user-data. An empty result means the provider sees no
// signing key material by any of the known transports.
func signingMaterialMarkersIn(userData string) []string {
	var found []string

	if strings.Contains(userData, pemPrivateKeyMarker) {
		found = append(found, pemPrivateKeyMarker)
	}

	for _, marker := range kubeadmCertificateTransportMarkers {
		if strings.Contains(userData, marker) {
			found = append(found, marker)
		}
	}

	for _, marker := range pkiPathMarkers {
		if strings.Contains(userData, marker) {
			found = append(found, marker)
		}
	}

	return found
}

// assertNoSigningMaterial fails when the rendered user-data carries cluster
// signing material by any transport, naming the markers so a regression points
// straight at what leaked.
func assertNoSigningMaterial(t *testing.T, userData, subject string) {
	t.Helper()

	assert.Empty(t, signingMaterialMarkersIn(userData),
		"%s must expose no cluster-signing material through provider user-data", subject)
}

// TestSigningMaterialGuardCatchesEveryPrivateKeyEncoding pins the guard's own
// reach. A guard written against one PEM spelling passes over a key that
// arrives in another encoding, so each supported encoding and each kubeadm
// certificate-transport setting is exercised individually.
func TestSigningMaterialGuardCatchesEveryPrivateKeyEncoding(t *testing.T) {
	t.Parallel()

	for name, sample := range map[string]string{
		"pkcs1 rsa":       "-----BEGIN RSA PRIVATE KEY-----\nMIIE\n-----END RSA PRIVATE KEY-----",
		"pkcs8":           "-----BEGIN PRIVATE KEY-----\nMIIE\n-----END PRIVATE KEY-----",
		"sec1 ec":         "-----BEGIN EC PRIVATE KEY-----\nMHc\n-----END EC PRIVATE KEY-----",
		"dsa":             "-----BEGIN DSA PRIVATE KEY-----\nMIIB\n-----END DSA PRIVATE KEY-----",
		"encrypted pkcs8": "-----BEGIN ENCRYPTED PRIVATE KEY-----\nMIIE\n-----END ENCRYPTED PRIVATE KEY-----",
		"openssh":         "-----BEGIN OPENSSH PRIVATE KEY-----\nb3Bl\n-----END OPENSSH PRIVATE KEY-----",
		"kubeadm certificateKey": "write_files:\n  - content: |\n" +
			"      certificateKey: 0123456789abcdef0123456789abcdef\n",
		"kubeadm upload-certs": "runcmd:\n  - kubeadm init --upload-certs\n",
		"ca key path":          "write_files:\n  - path: /etc/kubernetes/pki/ca.key\n",
	} {
		userData := "#cloud-config\n" + sample

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.NotEmpty(t, signingMaterialMarkersIn(userData),
				"a %s transport must be reported as signing material", name)
		})
	}
}

// TestSigningMaterialGuardAcceptsMaterialFreeUserData is the guard's negative
// control. Public material a node legitimately receives — an SSH authorized
// key, the CA discovery pin, a bootstrap token — must not trip it, otherwise an
// always-firing guard says nothing about the render it is protecting.
func TestSigningMaterialGuardAcceptsMaterialFreeUserData(t *testing.T) {
	t.Parallel()

	userData := "#cloud-config\n" +
		"ssh_authorized_keys:\n  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 operator\n" +
		"runcmd:\n" +
		"  - kubeadm join test-cluster-api.ksail.internal:6443" +
		" --token abcdef.0123456789abcdef" +
		" --discovery-token-ca-cert-hash sha256:0123456789abcdef\n"

	assert.Empty(t, signingMaterialMarkersIn(userData))
}
