// Package kubeconfigwriter serializes the minimal single-context kubeconfig
// cloud connectors hand to the operator: one cluster (server + CA), one
// bearer-token auth-info, one context. Shared so every connector emits the
// same shape instead of each cloud package growing its own copy.
package kubeconfigwriter

import (
	"fmt"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Write serializes a single-context kubeconfig for the given server, CA, and
// bearer token.
func Write(contextName, server string, caData []byte, token string) ([]byte, error) {
	config := clientcmdapi.NewConfig()
	config.Clusters[contextName] = &clientcmdapi.Cluster{
		Server:                   server,
		CertificateAuthorityData: caData,
	}
	config.AuthInfos[contextName] = &clientcmdapi.AuthInfo{Token: token}
	config.Contexts[contextName] = &clientcmdapi.Context{
		Cluster:  contextName,
		AuthInfo: contextName,
	}
	config.CurrentContext = contextName

	raw, err := clientcmd.Write(*config)
	if err != nil {
		return nil, fmt.Errorf("serializing kubeconfig: %w", err)
	}

	return raw, nil
}
