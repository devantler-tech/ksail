package eksprovisioner

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kubeconfigwriter"
)

// AWSClusterAPI is the narrow seam over the AWS-SDK-backed EKS client the
// connector uses, so tests can inject a fake without AWS credentials.
// *pkg/client/eks.Client satisfies it.
type AWSClusterAPI interface {
	// DescribeCluster returns the named cluster's control-plane details.
	DescribeCluster(ctx context.Context, name string) (*ekstypes.Cluster, error)
	// MintToken mints an EKS bearer token bound to the named cluster.
	MintToken(ctx context.Context, clusterName string) (string, error)
}

// Kubeconfig implements the clusterprovisioner.Connector capability for an
// EKS cluster: it builds a kubeconfig from the cluster's own control-plane
// endpoint and CA (public to the operator, no address rewrite needed) with a
// bearer token minted from the AWS credentials the operator runs under (IRSA
// or an instance role unless a client was injected). Tokens are short-lived
// by design — the operator calls Kubeconfig on each reconcile and the
// installers consume it immediately. It returns
// clustererr.ErrKubeconfigNotReady while the cluster is still provisioning
// so the caller requeues.
func (p *Provisioner) Kubeconfig(ctx context.Context, name string) ([]byte, error) {
	target := p.resolveName(name)
	if target == "" {
		return nil, fmt.Errorf("%w: no cluster name configured", ErrClusterNameRequired)
	}

	api, err := p.resolveAWSClient(ctx)
	if err != nil {
		return nil, err
	}

	cluster, err := api.DescribeCluster(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("eks describe cluster: %w", err)
	}

	endpoint, certificateAuthority, err := clusterConnection(target, cluster)
	if err != nil {
		return nil, err
	}

	token, err := api.MintToken(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("minting eks bearer token: %w", err)
	}

	raw, err := kubeconfigwriter.Write(
		fmt.Sprintf("eks_%s_%s", p.region, target),
		endpoint,
		certificateAuthority,
		token,
	)
	if err != nil {
		return nil, fmt.Errorf("eks kubeconfig: %w", err)
	}

	return raw, nil
}

// clusterConnection extracts the operator-reachable endpoint and decoded CA
// from a described cluster, returning clustererr.ErrKubeconfigNotReady until
// an ACTIVE cluster serves both.
func clusterConnection(name string, cluster *ekstypes.Cluster) (string, []byte, error) {
	if cluster == nil {
		return "", nil, fmt.Errorf(
			"%w: eks cluster %q has no payload",
			clustererr.ErrKubeconfigNotReady, name,
		)
	}

	endpoint := aws.ToString(cluster.Endpoint)

	var caCertificate string
	if cluster.CertificateAuthority != nil {
		caCertificate = aws.ToString(cluster.CertificateAuthority.Data)
	}

	if cluster.Status != ekstypes.ClusterStatusActive || endpoint == "" || caCertificate == "" {
		return "", nil, fmt.Errorf(
			"%w: eks cluster %q is %s",
			clustererr.ErrKubeconfigNotReady, name, cluster.Status,
		)
	}

	certificateAuthority, err := base64.StdEncoding.DecodeString(caCertificate)
	if err != nil {
		return "", nil, fmt.Errorf("decoding eks cluster CA certificate: %w", err)
	}

	return endpoint, certificateAuthority, nil
}

// resolveAWSClient returns the injected AWS client, lazily constructing and
// caching the real SDK-backed one when none was injected. Construction is
// deferred to first connector use (and a failed construction is not cached)
// because the CLI lifecycle paths drive eksctl and never need AWS
// credentials resolved.
func (p *Provisioner) resolveAWSClient(ctx context.Context) (AWSClusterAPI, error) {
	p.awsMu.Lock()
	defer p.awsMu.Unlock()

	if p.awsClient != nil {
		return p.awsClient, nil
	}

	client, err := eksclient.NewClientWithCredentialRequirement(
		ctx,
		p.region,
		p.requireCredentialValues,
		p.eksClientOptions...,
	)
	if err != nil {
		return nil, fmt.Errorf("creating aws eks client: %w", err)
	}

	p.awsClient = client

	return client, nil
}
