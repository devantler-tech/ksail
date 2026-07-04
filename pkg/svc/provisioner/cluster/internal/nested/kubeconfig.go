package nested

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ExtractContextKubeconfig reads the kubeconfig at path and returns a minified
// single-context kubeconfig for contextName: only that context and the cluster
// and auth-info it references, with CurrentContext set to it. The DinD-based
// provisioners (Kind, KWOK) write their nested cluster's entry into the shared
// host kubeconfig alongside the host context, so publishing the file as-is would
// hand the operator a config whose current-context points at the host. The
// operator builds its child-cluster client with an empty context (it relies on
// current-context), so the published Secret must carry exactly one, nested,
// context.
func ExtractContextKubeconfig(path, contextName string) ([]byte, error) {
	config, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig %s: %w", path, err)
	}

	kubeContext, hasContext := config.Contexts[contextName]
	if !hasContext {
		return nil, fmt.Errorf(
			"%w: context %q not found in %s",
			clustererr.ErrKubeconfigContextMissing, contextName, path,
		)
	}

	cluster, hasCluster := config.Clusters[kubeContext.Cluster]
	if !hasCluster {
		return nil, fmt.Errorf(
			"%w: cluster %q for context %q not found in %s",
			clustererr.ErrKubeconfigContextMissing, kubeContext.Cluster, contextName, path,
		)
	}

	minified := clientcmdapi.NewConfig()
	minified.Clusters[kubeContext.Cluster] = cluster

	if kubeContext.AuthInfo != "" {
		authInfo, hasAuthInfo := config.AuthInfos[kubeContext.AuthInfo]
		if !hasAuthInfo {
			return nil, fmt.Errorf(
				"%w: auth info %q for context %q not found in %s",
				clustererr.ErrKubeconfigContextMissing, kubeContext.AuthInfo, contextName, path,
			)
		}

		minified.AuthInfos[kubeContext.AuthInfo] = authInfo
	}

	minified.Contexts[contextName] = kubeContext
	minified.CurrentContext = contextName

	raw, err := clientcmd.Write(*minified)
	if err != nil {
		return nil, fmt.Errorf("serialize minified kubeconfig for context %q: %w", contextName, err)
	}

	return raw, nil
}

// PublishConnectorKubeconfig extracts the nested cluster's single-context kubeconfig from the
// shared host kubeconfig at kubeconfigPath and publishes it as a Secret under the Connection
// naming contract (namespace/secretName/key), so the operator's Connector can read it back. It is
// the shared publish flow for the DinD-based distributions (Kind, KWOK), whose on-disk kubeconfig
// is already pointed at the operator-reachable exposure address; it is published as-is after
// minifying. Idempotent: re-provisioning a cluster of the same name refreshes the credentials.
func PublishConnectorKubeconfig(
	ctx context.Context,
	clientset kubernetes.Interface,
	key, kubeconfigPath, contextName, namespace, secretName string,
	labels map[string]string,
) error {
	raw, err := ExtractContextKubeconfig(kubeconfigPath, contextName)
	if err != nil {
		return fmt.Errorf("extract nested kubeconfig: %w", err)
	}

	err = PublishKubeconfigSecret(ctx, clientset, namespace, secretName, key, raw, labels)
	if err != nil {
		return fmt.Errorf("publish kubeconfig secret: %w", err)
	}

	return nil
}

// PublishKubeconfigSecret upserts a Secret named secretName in namespace holding
// data under key. It is the write half of the Connector contract for DinD-based
// distributions (Kind, KWOK): the nested cluster's kubeconfig is written to a
// file in the DinD flow rather than published by a nested controller, so the
// provisioner publishes it to the host cluster itself so the operator can read
// it back through the Connector. The write is idempotent (see UpsertSecret) so a
// re-provision or re-reconcile refreshes the credentials in place.
func PublishKubeconfigSecret(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, secretName, key string,
	data []byte,
	labels map[string]string,
) error {
	if clientset == nil {
		return fmt.Errorf("%w: host clientset not set", clustererr.ErrKubeconfigPublishInvalid)
	}

	if len(data) == 0 {
		return fmt.Errorf("%w: kubeconfig data is empty", clustererr.ErrKubeconfigPublishInvalid)
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Data: map[string][]byte{key: data},
	}

	return UpsertSecret(ctx, clientset, secret)
}
