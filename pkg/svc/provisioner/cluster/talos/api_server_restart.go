package talosprovisioner

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"golang.org/x/sync/errgroup"
)

const (
	// apiServerRestartTimeout bounds the wait for every control plane's
	// kube-apiserver to restart onto a newly pushed endpoint and cert SAN set
	// and then stay up for apiServerStableWindow.
	apiServerRestartTimeout = 5 * time.Minute

	// apiServerStableWindow is how long a kube-apiserver must keep serving a
	// certificate valid for the endpoint, without a single failed handshake,
	// before the endpoint counts as settled. kube-apiserver reloads its serving
	// certificate from disk without restarting, so the new certificate can be
	// served shortly BEFORE the static-pod restart that the same config change
	// triggers; the window outlasts the kubelet's ~20 s static-pod refresh so
	// that restart lands inside it and resets the count.
	apiServerStableWindow = 45 * time.Second

	// apiServerProbeInterval spaces the TLS handshakes of the settle wait. It
	// is short so a restart's refused connections inside the window are seen.
	apiServerProbeInterval = 2 * time.Second

	// apiServerHandshakeTimeout bounds one connect plus TLS handshake of the
	// settle wait. It is independent of the probe interval so a slow but
	// healthy handshake is never misread as a failure.
	apiServerHandshakeTimeout = 10 * time.Second
)

var (
	// errAPIServerNotSettled reports that a kube-apiserver did not serve a
	// certificate valid for the endpoint for a whole stable window in time.
	errAPIServerNotSettled = errors.New("kube-apiserver did not settle on the endpoint")

	// errInvalidKubernetesCA reports a Kubernetes CA certificate that cannot be
	// parsed into a trust pool.
	errInvalidKubernetesCA = errors.New("kubernetes CA certificate is not valid PEM")
)

// waitForAPIServersServingEndpoint waits until the kube-apiserver of every
// control-plane server serves a certificate that chains to the cluster's
// Kubernetes CA and is valid for endpointIP, without interruption for
// apiServerStableWindow.
//
// Pushing a floating-IP endpoint in place changes kube-apiserver's cert SANs
// and flags, and Talos restarts it to apply them. A single successful probe
// right after the push can still reach the old process, so the next command
// hit `connection refused` once the restart began (#6032). Requiring a
// certificate valid for the new endpoint proves the new configuration is
// served, and the stable window proves the restart has finished.
func (p *Provisioner) waitForAPIServersServingEndpoint(
	ctx context.Context,
	controlPlaneServers []*hcloud.Server,
	endpointIP string,
) error {
	caPEM := p.kubernetesCAPEM()
	if len(caPEM) == 0 {
		return errFloatingIPConfigsUnavailable
	}

	_, _ = fmt.Fprintf(
		p.logWriter,
		"  ⏳ Waiting for kube-apiserver to restart on endpoint %s\n",
		endpointIP,
	)

	group, groupCtx := errgroup.WithContext(ctx)

	for _, server := range controlPlaneServers {
		nodeIP, err := hetznerNodeTalosAddress(server)
		if err != nil {
			return err
		}

		group.Go(func() error {
			return p.apiServerServingCheck(groupCtx, nodeIP, endpointIP, caPEM)
		})
	}

	err := group.Wait()
	if err != nil {
		return fmt.Errorf("wait for kube-apiserver on endpoint %s: %w", endpointIP, err)
	}

	return nil
}

// kubernetesCAPEM returns the cluster's Kubernetes CA certificate from the
// loaded control-plane config, or nil when it is not loaded.
func (p *Provisioner) kubernetesCAPEM() []byte {
	if p.talosConfigs == nil || p.talosConfigs.ControlPlane() == nil {
		return nil
	}

	ca := p.talosConfigs.ControlPlane().Cluster().IssuingCA()
	if ca == nil {
		return nil
	}

	return ca.Crt
}

// servingCertificateDialer returns a TLS dialer whose handshake succeeds only
// when the server's certificate chains to caPEM and is valid for serverName.
func servingCertificateDialer(serverName string, caPEM []byte) (*tls.Dialer, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, errInvalidKubernetesCA
	}

	return &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: apiServerHandshakeTimeout},
		Config: &tls.Config{
			RootCAs:    pool,
			ServerName: serverName,
			MinVersion: tls.VersionTLS12,
		},
	}, nil
}

// waitForServingCertificate waits until the Kubernetes API at address
// (host:port) completes a TLS handshake that verifies against
// caPEM for serverName on every probe for stableWindow, probing every
// interval. Any failed handshake restarts the window. It gives up after
// timeout, returning the last handshake error.
func waitForServingCertificate(
	ctx context.Context,
	address, serverName string,
	caPEM []byte,
	timeout, stableWindow, interval time.Duration,
) error {
	dialer, err := servingCertificateDialer(serverName, caPEM)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	deadline, _ := ctx.Deadline()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var (
		settledSince time.Time
		lastErr      error
	)

	for {
		conn, dialErr := dialer.DialContext(ctx, "tcp", address)
		if dialErr == nil {
			_ = conn.Close()

			if settledSince.IsZero() {
				settledSince = time.Now()
			}

			if time.Since(settledSince) >= stableWindow {
				return nil
			}
		} else {
			settledSince = time.Time{}

			// A dial cut short by the deadline says nothing about the server;
			// keep the handshake error that explains why it never settled.
			if time.Now().Before(deadline) {
				lastErr = dialErr
			}
		}

		select {
		case <-ctx.Done():
			if lastErr == nil {
				lastErr = ctx.Err()
			}

			return fmt.Errorf(
				"%w: %s did not serve a certificate valid for %s for %s within %s: %w",
				errAPIServerNotSettled, address, serverName, stableWindow, timeout, lastErr,
			)
		case <-ticker.C:
		}
	}
}
