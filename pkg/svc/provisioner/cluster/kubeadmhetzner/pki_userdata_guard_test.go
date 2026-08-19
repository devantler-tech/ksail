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

// pemPrivateKeyLabels are the PEM block labels the guard must reach. Each is a
// label only: the fixtures below wrap them around a placeholder body, so no
// test carries key-shaped bytes.
func pemPrivateKeyLabels() []string {
	return []string{
		"RSA PRIVATE KEY",
		"PRIVATE KEY",
		"EC PRIVATE KEY",
		"DSA PRIVATE KEY",
		"ENCRYPTED PRIVATE KEY",
		"OPENSSH PRIVATE KEY",
	}
}

// kubeadmCertificateTransportMarkers are the kubeadm settings that move control
// plane signing material between nodes. `--upload-certs` stores the cluster PKI
// in a cluster Secret and `certificateKey` is the symmetric key that decrypts
// it, so either one in provider user-data hands the provider the cluster
// identity even though no PEM block appears.
func kubeadmCertificateTransportMarkers() []string {
	return []string{
		"certificateKey",
		"certificate-key",
		"upload-certs",
		"uploadCerts",
	}
}

// pkiPathMarkers are the on-disk locations of the private halves of the cluster
// PKI. kubeadm mints these on the initial control plane; a path appearing in
// user-data means the renderer is writing the material rather than letting
// kubeadm generate it.
func pkiPathMarkers() []string {
	return []string{
		"/etc/kubernetes/pki/ca.key",
		"/etc/kubernetes/pki/front-proxy-ca.key",
		"/etc/kubernetes/pki/etcd/ca.key",
		"/etc/kubernetes/pki/sa.key",
	}
}

// pemBlock wraps a placeholder body in a PEM block carrying the given label.
func pemBlock(label string) string {
	return "-----BEGIN " + label + "-----\nredacted\n-----END " + label + "-----"
}

// signingMaterialMarkersIn reports every marker of cluster-signing material
// present in the rendered user-data. An empty result means the provider sees no
// signing key material by any of the known transports.
func signingMaterialMarkersIn(userData string) []string {
	var found []string

	if strings.Contains(userData, pemPrivateKeyMarker) {
		found = append(found, pemPrivateKeyMarker)
	}

	for _, marker := range kubeadmCertificateTransportMarkers() {
		if strings.Contains(userData, marker) {
			found = append(found, marker)
		}
	}

	for _, marker := range pkiPathMarkers() {
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
// arrives in another encoding, so every supported label is exercised.
func TestSigningMaterialGuardCatchesEveryPrivateKeyEncoding(t *testing.T) {
	t.Parallel()

	for _, label := range pemPrivateKeyLabels() {
		userData := "#cloud-config\nwrite_files:\n  - content: |\n      " + pemBlock(label) + "\n"

		t.Run(label, func(t *testing.T) {
			t.Parallel()

			assert.NotEmpty(t, signingMaterialMarkersIn(userData),
				"a %s block must be reported as signing material", label)
		})
	}
}

// TestSigningMaterialGuardCatchesKubeadmCertificateTransports pins the reach
// over kubeadm's own certificate sharing, which moves signing material without
// emitting a PEM block at all.
func TestSigningMaterialGuardCatchesKubeadmCertificateTransports(t *testing.T) {
	t.Parallel()

	for name, sample := range map[string]string{
		"certificateKey": "write_files:\n  - content: |\n" +
			"      certificateKey: " + strings.Repeat("0", 64) + "\n",
		"upload-certs": "runcmd:\n  - kubeadm init --upload-certs\n",
		"ca key path":  "write_files:\n  - path: /etc/kubernetes/pki/ca.key\n",
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
