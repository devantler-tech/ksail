package talosprovisioner

import (
	"context"
	"fmt"
	"strings"

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// readScaleDownNodes binds every owned control plane to a distinct Ready native
// Node through its immutable provider ID, using the caller's original API route.
func (p *Provisioner) readScaleDownNodes(
	ctx context.Context,
	clientset kubernetes.Interface,
	servers []*hcloud.Server,
) ([]corev1.Node, error) {
	ctx, cancel := context.WithTimeout(ctx, removalReadTimeout)
	defer cancel()

	ready, err := clientset.CoreV1().RESTClient().Get().AbsPath("/readyz").DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: original API route is not ready: %w",
			ErrControlPlaneScaleDownUnsafe,
			err,
		)
	}

	if strings.TrimSpace(string(ready)) != "ok" {
		return nil, fmt.Errorf(
			"%w: original API route did not report ready",
			ErrControlPlaneScaleDownUnsafe,
		)
	}

	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("read scale-down nodes: %w", err)
	}

	seen := make(map[string]bool, len(servers))
	for _, server := range servers {
		node, bindErr := uniqueNodeForServer(nodes.Items, server)
		if bindErr != nil {
			return nil, bindErr
		}

		if seen[string(node.UID)] {
			return nil, fmt.Errorf(
				"%w: multiple servers resolve to one native Node",
				ErrControlPlaneScaleDownUnsafe,
			)
		}

		seen[string(node.UID)] = true

		err = newNodeReady(nodes.Items, replacementTarget{ServerID: server.ID, NodeUID: node.UID})
		if err != nil {
			return nil, err
		}
	}

	return nodes.Items, nil
}
