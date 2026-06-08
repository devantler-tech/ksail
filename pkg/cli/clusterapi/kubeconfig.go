package clusterapi

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Ensure the local backend can export a kubeconfig.
var _ api.KubeconfigProvider = (*Service)(nil)

// Kubeconfig exports a portable, single-context kubeconfig for the named local cluster. It resolves
// the cluster's context in the user's kubeconfig (via the same name→context matching the resource
// browser uses) and emits a new kubeconfig containing only that context plus the cluster and auth
// info it references — so the downloaded file targets exactly the one cluster.
func (s *Service) Kubeconfig(_ context.Context, _, name string) ([]byte, error) {
	kubeconfigPath := s.kubeconfigPath()

	contextName, err := contextForCluster(kubeconfigPath, name)
	if err != nil {
		return nil, err
	}

	full, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %q: %w", kubeconfigPath, err)
	}

	contextEntry, ok := full.Contexts[contextName]
	if !ok {
		return nil, fmt.Errorf("%w: context %q not found", api.ErrNotFound, contextName)
	}

	minimal := clientcmdapi.NewConfig()
	minimal.CurrentContext = contextName
	minimal.Contexts[contextName] = contextEntry

	if cluster, found := full.Clusters[contextEntry.Cluster]; found {
		minimal.Clusters[contextEntry.Cluster] = cluster
	}

	if authInfo, found := full.AuthInfos[contextEntry.AuthInfo]; found {
		minimal.AuthInfos[contextEntry.AuthInfo] = authInfo
	}

	out, err := clientcmd.Write(*minimal)
	if err != nil {
		return nil, fmt.Errorf("serialize kubeconfig: %w", err)
	}

	return out, nil
}
