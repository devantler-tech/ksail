package hetznerbase

import (
	"fmt"
	"net"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"k8s.io/client-go/tools/clientcmd"
)

// kubeAPIPort is the port the distributions' API server listens on (both k3s
// and kubeadm default to the standard secure port).
const kubeAPIPort = "6443"

// apiServerEndpoint derives the externally reachable API-server URL from the
// created server's public IPv4 — the endpoint the retrieved kubeconfig is
// rewritten to. Both k3s and kubeadm include the node's own addresses in the
// API server's serving-certificate SANs, so the node's public IPv4 verifies
// without threading extra SANs through the user_data; a stable floating-IP
// endpoint (which does need that threading) is the follow-up increment
// (devantler-tech/ksail#5725).
func apiServerEndpoint(server *hcloud.Server) (string, error) {
	address, err := publicIPv4(server)
	if err != nil {
		return "", err
	}

	return "https://" + net.JoinHostPort(address.String(), kubeAPIPort), nil
}

// rewriteKubeconfigEndpoint rewrites every cluster's server URL in the raw
// kubeconfig to endpoint. The distribution writes the kubeconfig for use on
// the node itself (a loopback or private address); external access goes to
// the node's public address instead.
func rewriteKubeconfigEndpoint(kubeconfigBytes []byte, endpoint string) ([]byte, error) {
	kubeConfig, err := clientcmd.Load(kubeconfigBytes)
	if err != nil {
		return nil, fmt.Errorf("parse retrieved kubeconfig: %w", err)
	}

	// clientcmd.Load tolerates empty/partial input (e.g. a file read mid-write
	// on the node), so guard explicitly — a clusterless kubeconfig would
	// otherwise be persisted with no endpoint rewritten and no error surfaced.
	if len(kubeConfig.Clusters) == 0 {
		return nil, ErrKubeconfigNoClusters
	}

	for name := range kubeConfig.Clusters {
		kubeConfig.Clusters[name].Server = endpoint
	}

	result, err := clientcmd.Write(*kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("serialize rewritten kubeconfig: %w", err)
	}

	return result, nil
}

// persistKubeconfig merges the raw kubeconfig into the Base's kubeconfig
// destination (expanding a leading tilde) and returns the expanded path, the
// same house pattern the other distributions use (k8s.MergeKubeconfig creates
// the file and its directory when missing and preserves unrelated entries).
func (b *Base) persistKubeconfig(kubeconfig []byte) (string, error) {
	destPath, err := fsutil.ExpandHomePath(b.KubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("expand kubeconfig path: %w", err)
	}

	err = k8s.MergeKubeconfig(destPath, kubeconfig)
	if err != nil {
		return "", fmt.Errorf("merge kubeconfig into %q: %w", destPath, err)
	}

	return destPath, nil
}
