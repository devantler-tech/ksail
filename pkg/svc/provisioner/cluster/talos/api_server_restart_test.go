package talosprovisioner_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"sync/atomic"
	"testing"
	"time"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// settleEndpointIP is the floating IP the settle wait verifies the serving
	// certificate for.
	settleEndpointIP = "192.0.2.10"
	// settleTimeout bounds every settle-wait test that is expected to fail.
	settleTimeout = 400 * time.Millisecond
	// settleInterval keeps the probe loop fast enough to see short phases.
	settleInterval = 10 * time.Millisecond
)

// testCA is a throwaway certificate authority for the settle-wait tests.
type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  []byte
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "kubernetes"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	return &testCA{
		cert: cert,
		key:  key,
		pem:  pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
	}
}

// serving issues a kube-apiserver serving certificate for the given IP SANs.
func (ca *testCA) serving(t *testing.T, ips ...string) *tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "kube-apiserver"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	for _, ip := range ips {
		template.IPAddresses = append(template.IPAddresses, net.ParseIP(ip))
	}

	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)

	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// startServingServer starts a TLS server on loopback that serves whatever
// certificate is currently stored in served, standing in for a kube-apiserver
// whose certificate changes when it restarts.
func startServingServer(t *testing.T, served *atomic.Pointer[tls.Certificate]) string {
	t.Helper()

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return served.Load(), nil
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	ctx := t.Context()

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}

			go func() {
				_ = conn.(*tls.Conn).HandshakeContext(
					ctx,
				) //nolint:forcetypeassert // tls.Listen yields *tls.Conn
				_ = conn.Close()
			}()
		}
	}()

	return listener.Addr().String()
}

// TestWaitForServingCertificate_SettlesOnCertificateForEndpoint verifies an
// API server already serving a CA-signed certificate for the endpoint settles
// once the stable window has passed.
func TestWaitForServingCertificate_SettlesOnCertificateForEndpoint(t *testing.T) {
	t.Parallel()

	authority := newTestCA(t)

	var served atomic.Pointer[tls.Certificate]
	served.Store(authority.serving(t, "127.0.0.1", settleEndpointIP))

	address := startServingServer(t, &served)
	window := 100 * time.Millisecond
	start := time.Now()

	err := talosprovisioner.WaitForServingCertificateForTest(
		t.Context(),
		address,
		settleEndpointIP,
		authority.pem,
		5*time.Second,
		window,
		settleInterval,
	)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), window)
}

// TestWaitForServingCertificate_WaitsForRestartOntoEndpointCertificate pins
// #6032: the old kube-apiserver still answers right after the in-place push,
// with a certificate that does not cover the floating IP, so the wait must
// hold until the restarted server serves the new certificate.
func TestWaitForServingCertificate_WaitsForRestartOntoEndpointCertificate(t *testing.T) {
	t.Parallel()

	authority := newTestCA(t)

	var served atomic.Pointer[tls.Certificate]
	served.Store(authority.serving(t, "127.0.0.1"))

	address := startServingServer(t, &served)
	restartAfter := 150 * time.Millisecond
	window := 100 * time.Millisecond

	restarted := authority.serving(t, "127.0.0.1", settleEndpointIP)

	time.AfterFunc(restartAfter, func() { served.Store(restarted) })

	start := time.Now()

	err := talosprovisioner.WaitForServingCertificateForTest(
		t.Context(),
		address,
		settleEndpointIP,
		authority.pem,
		5*time.Second,
		window,
		settleInterval,
	)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), restartAfter+window,
		"the wait must not settle on the pre-restart certificate")
}

// TestWaitForServingCertificate_FailedHandshakeRestartsWindow verifies a
// restart that begins after the new certificate is already served (it is
// reloaded from disk before the static-pod restart) resets the stable window.
func TestWaitForServingCertificate_FailedHandshakeRestartsWindow(t *testing.T) {
	t.Parallel()

	authority := newTestCA(t)
	good := authority.serving(t, "127.0.0.1", settleEndpointIP)

	var served atomic.Pointer[tls.Certificate]
	served.Store(good)

	address := startServingServer(t, &served)
	reloadPhase := 100 * time.Millisecond
	restartPhase := 100 * time.Millisecond
	window := 250 * time.Millisecond

	restarting := authority.serving(t, "127.0.0.1")

	time.AfterFunc(reloadPhase, func() { served.Store(restarting) })
	time.AfterFunc(reloadPhase+restartPhase, func() { served.Store(good) })

	start := time.Now()

	err := talosprovisioner.WaitForServingCertificateForTest(
		t.Context(),
		address,
		settleEndpointIP,
		authority.pem,
		5*time.Second,
		window,
		settleInterval,
	)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), reloadPhase+restartPhase+window,
		"a failed handshake inside the window must restart it")
}

// TestWaitForServingCertificate_FailsWithoutEndpointSAN verifies a server
// that never serves a certificate for the endpoint fails with the handshake
// error rather than settling.
func TestWaitForServingCertificate_FailsWithoutEndpointSAN(t *testing.T) {
	t.Parallel()

	authority := newTestCA(t)

	var served atomic.Pointer[tls.Certificate]
	served.Store(authority.serving(t, "127.0.0.1"))

	address := startServingServer(t, &served)

	err := talosprovisioner.WaitForServingCertificateForTest(
		t.Context(), address, settleEndpointIP, authority.pem, settleTimeout, 0, settleInterval,
	)
	require.ErrorContains(t, err, "kube-apiserver did not settle on the endpoint")

	var hostnameErr x509.HostnameError

	assert.ErrorAs(t, err, &hostnameErr)
}

// TestWaitForServingCertificate_FailsOnUntrustedCertificate verifies a
// certificate for the endpoint from another CA never counts as the cluster's
// kube-apiserver.
func TestWaitForServingCertificate_FailsOnUntrustedCertificate(t *testing.T) {
	t.Parallel()

	clusterCA := newTestCA(t)
	otherCA := newTestCA(t)

	var served atomic.Pointer[tls.Certificate]
	served.Store(otherCA.serving(t, "127.0.0.1", settleEndpointIP))

	address := startServingServer(t, &served)

	err := talosprovisioner.WaitForServingCertificateForTest(
		t.Context(), address, settleEndpointIP, clusterCA.pem, settleTimeout, 0, settleInterval,
	)
	require.ErrorContains(t, err, "kube-apiserver did not settle on the endpoint")

	var authorityErr x509.UnknownAuthorityError

	assert.ErrorAs(t, err, &authorityErr)
}

// TestWaitForServingCertificate_RejectsInvalidCA verifies an unusable CA
// fails at once instead of probing.
func TestWaitForServingCertificate_RejectsInvalidCA(t *testing.T) {
	t.Parallel()

	err := talosprovisioner.WaitForServingCertificateForTest(
		t.Context(), "127.0.0.1:1", settleEndpointIP, []byte("not a certificate"),
		settleTimeout, 0, settleInterval,
	)
	require.ErrorContains(t, err, "kubernetes CA certificate is not valid PEM")
}
