package eks_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errBoom = errors.New("boom")

// fakeDescriber scripts the DescribeCluster seam.
type fakeDescriber struct {
	out *awseks.DescribeClusterOutput
	err error
}

func (f fakeDescriber) DescribeCluster(
	_ context.Context,
	_ *awseks.DescribeClusterInput,
	_ ...func(*awseks.Options),
) (*awseks.DescribeClusterOutput, error) {
	return f.out, f.err
}

// fakePresigner scripts the STS presign seam.
type fakePresigner struct {
	request *v4.PresignedHTTPRequest
	err     error
}

func (f fakePresigner) PresignGetCallerIdentity(
	_ context.Context,
	_ *sts.GetCallerIdentityInput,
	_ ...func(*sts.PresignOptions),
) (*v4.PresignedHTTPRequest, error) {
	return f.request, f.err
}

// newTestClient wires a Client from injected seams so no AWS configuration
// is resolved.
func newTestClient(
	t *testing.T,
	describer fakeDescriber,
	presigner fakePresigner,
) *eksclient.Client {
	t.Helper()

	client, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(describer),
		eksclient.WithCallerIdentityPresigner(presigner),
	)
	require.NoError(t, err)

	return client
}

func TestDescribeClusterReturnsPayload(t *testing.T) {
	t.Parallel()

	cluster := &ekstypes.Cluster{Status: ekstypes.ClusterStatusActive}
	client := newTestClient(
		t,
		fakeDescriber{out: &awseks.DescribeClusterOutput{Cluster: cluster}, err: nil},
		fakePresigner{request: nil, err: nil},
	)

	got, err := client.DescribeCluster(t.Context(), "eks-default")
	require.NoError(t, err)
	assert.Same(t, cluster, got)
}

func TestDescribeClusterWrapsError(t *testing.T) {
	t.Parallel()

	client := newTestClient(
		t,
		fakeDescriber{out: nil, err: errBoom},
		fakePresigner{request: nil, err: nil},
	)

	_, err := client.DescribeCluster(t.Context(), "eks-default")
	require.ErrorIs(t, err, errBoom)
	require.ErrorContains(t, err, "describing eks cluster eks-default")
}

func TestDescribeClusterRejectsEmptyResponse(t *testing.T) {
	t.Parallel()

	client := newTestClient(
		t,
		fakeDescriber{out: &awseks.DescribeClusterOutput{Cluster: nil}, err: nil},
		fakePresigner{request: nil, err: nil},
	)

	_, err := client.DescribeCluster(t.Context(), "eks-default")
	require.ErrorIs(t, err, eksclient.ErrClusterNotFound)
}

func TestMintTokenEncodesPresignedURL(t *testing.T) {
	t.Parallel()

	presignedURL := "https://sts.eu-central-1.amazonaws.com/?Action=GetCallerIdentity"
	client := newTestClient(
		t,
		fakeDescriber{out: nil, err: nil},
		fakePresigner{request: &v4.PresignedHTTPRequest{URL: presignedURL}, err: nil},
	)

	token, err := client.MintToken(t.Context(), "eks-default")
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(token, "k8s-aws-v1."))

	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "k8s-aws-v1."))
	require.NoError(t, err)
	assert.Equal(t, presignedURL, string(decoded))
}

func TestMintTokenPropagatesPresignError(t *testing.T) {
	t.Parallel()

	client := newTestClient(
		t,
		fakeDescriber{out: nil, err: nil},
		fakePresigner{request: nil, err: errBoom},
	)

	_, err := client.MintToken(t.Context(), "eks-default")
	require.ErrorIs(t, err, errBoom)
}

// TestMintTokenSignsClusterBindingHeaders runs the real STS presigner with
// static credentials (signing is offline — no network) and pins the token
// contract EKS validates: the cluster-binding header is signed and the
// expiry cap is present.
func TestMintTokenSignsClusterBindingHeaders(t *testing.T) {
	t.Parallel()

	stsClient := sts.New(sts.Options{
		Region:      "eu-central-1",
		Credentials: credentials.NewStaticCredentialsProvider("AKIDEXAMPLE", "test-secret", ""),
	})
	client, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(fakeDescriber{out: nil, err: nil}),
		eksclient.WithCallerIdentityPresigner(sts.NewPresignClient(stsClient)),
	)
	require.NoError(t, err)

	token, err := client.MintToken(t.Context(), "eks-default")
	require.NoError(t, err)

	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "k8s-aws-v1."))
	require.NoError(t, err)

	signedURL, err := url.Parse(string(decoded))
	require.NoError(t, err)

	query := signedURL.Query()
	assert.Equal(t, "GetCallerIdentity", query.Get("Action"))
	assert.Equal(t, "60", query.Get("X-Amz-Expires"))
	assert.Contains(t, query.Get("X-Amz-SignedHeaders"), "x-k8s-aws-id")
}
