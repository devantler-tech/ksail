package gkeprovisioner

import (
	"context"
	"encoding/base64"
	"fmt"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// cloudPlatformScope is the OAuth scope GKE accepts for cluster API access;
// the default token source mints tokens with it when none is injected.
const cloudPlatformScope = "https://www.googleapis.com/auth/cloud-platform"

// Kubeconfig implements the clusterprovisioner.Connector capability for a GKE
// cluster: it builds a kubeconfig from the cluster's own control-plane
// endpoint and CA (public to the operator, no address rewrite needed) with a
// bearer token minted from the provisioner's token source (Google application
// default credentials unless one was injected). Tokens are short-lived by
// design — the operator calls Kubeconfig on each reconcile and the installers
// consume it immediately. It returns clustererr.ErrKubeconfigNotReady while
// the cluster is still provisioning so the caller requeues.
func (p *Provisioner) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	target := p.resolveName(name)
	if target == "" {
		return nil, fmt.Errorf("%w: no cluster name configured", ErrClusterNotFound)
	}

	location, err := p.resolveLocation(ctx, target)
	if err != nil {
		return nil, err
	}

	cluster, err := p.client.GetCluster(ctx, p.project, location, target)
	if err != nil {
		return nil, fmt.Errorf("gke get cluster: %w", err)
	}

	endpoint := cluster.GetEndpoint()
	caCertificate := cluster.GetMasterAuth().GetClusterCaCertificate()

	if cluster.GetStatus() != containerpb.Cluster_RUNNING || endpoint == "" || caCertificate == "" {
		return nil, fmt.Errorf(
			"%w: gke cluster %q is %s",
			clustererr.ErrKubeconfigNotReady, target, cluster.GetStatus(),
		)
	}

	certificateAuthority, err := base64.StdEncoding.DecodeString(caCertificate)
	if err != nil {
		return nil, fmt.Errorf("decoding gke cluster CA certificate: %w", err)
	}

	token, err := p.accessToken(ctx)
	if err != nil {
		return nil, err
	}

	return writeKubeconfig(
		fmt.Sprintf("gke_%s_%s_%s", p.project, location, target),
		"https://"+endpoint,
		certificateAuthority,
		token,
	)
}

// accessToken mints a bearer token from the injected token source, falling
// back to Google application default credentials (workload identity or a
// mounted service-account key in the operator pod). The resolved default
// source is cached so credential discovery (env/file checks, metadata-server
// probing) runs once, not on every reconcile; a failed discovery is not
// cached, so a transient ADC error is retried on the next call.
func (p *Provisioner) accessToken(ctx context.Context) (string, error) {
	source, err := p.resolveTokenSource(ctx)
	if err != nil {
		return "", err
	}

	token, err := source.Token()
	if err != nil {
		return "", fmt.Errorf("minting gke access token: %w", err)
	}

	return token.AccessToken, nil
}

// resolveTokenSource returns the injected token source, lazily resolving and
// caching Google application default credentials when none was injected.
func (p *Provisioner) resolveTokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()

	if p.tokenSource != nil {
		return p.tokenSource, nil
	}

	source, err := google.DefaultTokenSource(ctx, cloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("resolving google default credentials: %w", err)
	}

	p.tokenSource = source

	return source, nil
}

// writeKubeconfig serializes a single-context kubeconfig for the given
// server, CA, and bearer token.
func writeKubeconfig(contextName, server string, caData []byte, token string) ([]byte, error) {
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
		return nil, fmt.Errorf("serializing gke kubeconfig: %w", err)
	}

	return raw, nil
}
