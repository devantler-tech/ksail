package eks_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
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

// fakeIdentityGetter scripts the read-only STS identity query used by EKS ownership checks.
type fakeIdentityGetter struct {
	out *sts.GetCallerIdentityOutput
	err error
}

func (f fakeIdentityGetter) GetCallerIdentity(
	_ context.Context,
	_ *sts.GetCallerIdentityInput,
	_ ...func(*sts.Options),
) (*sts.GetCallerIdentityOutput, error) {
	return f.out, f.err
}

func (f fakePresigner) PresignGetCallerIdentity(
	_ context.Context,
	_ *sts.GetCallerIdentityInput,
	_ ...func(*sts.PresignOptions),
) (*v4.PresignedHTTPRequest, error) {
	return f.request, f.err
}

func newCallerIdentityServer(
	t *testing.T,
	authorization chan<- string,
) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		authorization <- request.Header.Get("Authorization")

		writer.Header().Set("Content-Type", "text/xml")
		_, _ = writer.Write([]byte(
			`<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">` +
				`<GetCallerIdentityResult><Account>123456789012</Account>` +
				`<Arn>arn:aws:iam::123456789012:user/ksail-test</Arn>` +
				`<UserId>KSailTest</UserId></GetCallerIdentityResult>` +
				`<ResponseMetadata><RequestId>test-request</RequestId></ResponseMetadata>` +
				`</GetCallerIdentityResponse>`,
		))
	}))
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
		eksclient.WithCallerIdentityGetter(fakeIdentityGetter{
			out: &sts.GetCallerIdentityOutput{Account: aws.String("123456789012")},
		}),
	)
	require.NoError(t, err)

	return client
}

func TestCallerAccountIDReturnsCurrentAccount(t *testing.T) {
	t.Parallel()

	client, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithCallerIdentityPresigner(fakePresigner{}),
		eksclient.WithCallerIdentityGetter(fakeIdentityGetter{
			out: &sts.GetCallerIdentityOutput{Account: aws.String("123456789012")},
		}),
	)
	require.NoError(t, err)

	accountID, err := client.CallerAccountID(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "123456789012", accountID)
}

func TestCallerAccountIDRejectsMissingAccount(t *testing.T) {
	t.Parallel()

	for name, output := range map[string]*sts.GetCallerIdentityOutput{
		"nil response":  nil,
		"nil account":   {},
		"empty account": {Account: aws.String("")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			client, err := eksclient.NewClient(
				t.Context(),
				"eu-central-1",
				eksclient.WithClusterDescriber(fakeDescriber{}),
				eksclient.WithCallerIdentityPresigner(fakePresigner{}),
				eksclient.WithCallerIdentityGetter(fakeIdentityGetter{out: output}),
			)
			require.NoError(t, err)

			_, err = client.CallerAccountID(t.Context())
			require.Error(t, err)
			assert.ErrorContains(t, err, "account ID")
		})
	}
}

func TestCallerAccountIDWrapsSTSFailure(t *testing.T) {
	t.Parallel()

	client, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithCallerIdentityPresigner(fakePresigner{}),
		eksclient.WithCallerIdentityGetter(fakeIdentityGetter{err: errBoom}),
	)
	require.NoError(t, err)

	_, err = client.CallerAccountID(t.Context())
	require.ErrorIs(t, err, errBoom)
	assert.ErrorContains(t, err, "getting AWS caller identity")
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

// TestWithCredentialValues_StaticCredentialsOverrideAmbientIdentity verifies
// explicit static credentials win over ambient identity.
func TestWithCredentialValues_StaticCredentialsOverrideAmbientIdentity(t *testing.T) {
	// Not parallel: t.Setenv changes the process environment.
	missingConfigDir := t.TempDir()
	t.Setenv("AWS_PROFILE", "stale-profile")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(missingConfigDir, "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(missingConfigDir, "missing-credentials"))
	t.Setenv("AWS_ACCESS_KEY_ID", "STALEAMBIENT")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "stale-ambient-secret")

	client, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithCredentialValues(
			"ignored-profile",
			"SELECTEDACCESS",
			"selected-secret",
			"selected-session",
		),
	)
	require.NoError(t, err)

	token, err := client.MintToken(t.Context(), "eks-default")
	require.NoError(t, err)
	assertTokenCredentialPrefix(t, token, "SELECTEDACCESS/")
}

// TestWithCredentialValues_StaticCredentialsPreserveEndpointConfig verifies explicit static
// credentials replace only AWS identity configuration. Non-credential settings loaded from the
// environment must still reach the real SDK clients, even when an unrelated ambient profile is
// stale, and construction must not mutate the process environment.
func TestWithCredentialValues_StaticCredentialsPreserveEndpointConfig(t *testing.T) {
	// Not parallel: t.Setenv changes the process environment.
	authorization := make(chan string, 1)
	server := newCallerIdentityServer(t, authorization)

	defer server.Close()

	missingConfigDir := t.TempDir()
	t.Setenv("AWS_PROFILE", "stale-profile")
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(missingConfigDir, "missing-config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(missingConfigDir, "missing-credentials"))
	t.Setenv("AWS_ACCESS_KEY_ID", "STALEAMBIENT")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "stale-ambient-secret")
	t.Setenv("AWS_ENDPOINT_URL", server.URL)
	t.Setenv("AWS_ENDPOINT_URL_STS", server.URL)
	t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")

	environmentBefore := append([]string(nil), os.Environ()...)

	client, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithCredentialValues(
			"ignored-profile",
			"SELECTEDACCESS",
			"selected-secret",
			"selected-session",
		),
	)
	require.NoError(t, err)

	queryCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	accountID, err := client.CallerAccountID(queryCtx)
	require.NoError(t, err)
	assert.Equal(t, "123456789012", accountID)
	assert.Equal(t, environmentBefore, os.Environ())

	select {
	case header := <-authorization:
		assert.Contains(t, header, "Credential=SELECTEDACCESS/")
	case <-queryCtx.Done():
		require.Fail(t, "STS endpoint did not receive a request")
	}
}

func TestWithAWSConfigPreservesFrozenEndpointAndCredentials(t *testing.T) {
	t.Parallel()

	authorization := make(chan string, 1)
	server := newCallerIdentityServer(t, authorization)

	defer server.Close()

	client, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithAWSConfig(aws.Config{
			Region: "stale-region",
			Credentials: credentials.NewStaticCredentialsProvider(
				"FROZENACCESS",
				"frozen-secret",
				"",
			),
			HTTPClient:   server.Client(),
			BaseEndpoint: aws.String(server.URL),
		}),
	)
	require.NoError(t, err)

	accountID, err := client.CallerAccountID(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "123456789012", accountID)

	select {
	case header := <-authorization:
		assert.Contains(t, header, "Credential=FROZENACCESS/")
		assert.Contains(t, header, "/eu-central-1/sts/aws4_request")
	case <-t.Context().Done():
		require.Fail(t, "STS endpoint did not receive a request")
	}
}

// TestWithCredentialValues_ProfileOverridesAmbientStaticCredentials verifies an
// explicit profile wins over ambient static credentials.
func TestWithCredentialValues_ProfileOverridesAmbientStaticCredentials(t *testing.T) {
	// Not parallel: t.Setenv changes the process environment.
	credentialsFile := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(
		credentialsFile,
		[]byte(
			"[selected-profile]\naws_access_key_id = PROFILEACCESS\naws_secret_access_key = profile-secret\n",
		),
		0o600,
	))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsFile)
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(t.TempDir(), "config"))
	t.Setenv("AWS_ACCESS_KEY_ID", "STALEAMBIENT")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "stale-ambient-secret")

	client, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithCredentialValues("selected-profile", "", "", ""),
	)
	require.NoError(t, err)

	token, err := client.MintToken(t.Context(), "eks-default")
	require.NoError(t, err)
	assertTokenCredentialPrefix(t, token, "PROFILEACCESS/")
}

// TestWithCredentialValues_RejectsPartialStaticCredentials verifies incomplete explicit key pairs fail closed.
func TestWithCredentialValues_RejectsPartialStaticCredentials(t *testing.T) {
	t.Parallel()

	_, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithCallerIdentityPresigner(fakePresigner{}),
		eksclient.WithCredentialValues("", "access-without-secret", "", ""),
	)
	require.ErrorIs(t, err, eksclient.ErrIncompleteStaticCredentials)
}

// TestRequireCredentialValuesRejectsAmbientFallback verifies required explicit
// credentials cannot fall back to ambient identity.
func TestRequireCredentialValuesRejectsAmbientFallback(t *testing.T) {
	t.Parallel()

	_, err := eksclient.NewClient(
		t.Context(),
		"eu-central-1",
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithCallerIdentityPresigner(fakePresigner{}),
		eksclient.WithCredentialValues("", "", "", ""),
		eksclient.RequireCredentialValues(),
	)
	require.ErrorIs(t, err, eksclient.ErrExplicitCredentialsUnavailable)
}

// TestNewClientWithCredentialRequirementRejectsAmbientFallbackWhenRequired
// verifies required construction rejects empty resolved credentials.
func TestNewClientWithCredentialRequirementRejectsAmbientFallbackWhenRequired(t *testing.T) {
	t.Parallel()

	base := []eksclient.Option{
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithCallerIdentityPresigner(fakePresigner{}),
		eksclient.WithCredentialValues("", "", "", ""),
	}
	_, err := eksclient.NewClientWithCredentialRequirement(
		t.Context(), "eu-central-1", true, base...,
	)
	require.ErrorIs(t, err, eksclient.ErrExplicitCredentialsUnavailable)
	require.Len(t, base, 3, "the caller-owned option slice must remain unchanged")
}

// TestNewClientWithCredentialRequirementPreservesDefaultChainWhenOptional
// verifies optional construction retains the AWS default chain.
func TestNewClientWithCredentialRequirementPreservesDefaultChainWhenOptional(t *testing.T) {
	t.Parallel()

	base := []eksclient.Option{
		eksclient.WithClusterDescriber(fakeDescriber{}),
		eksclient.WithCallerIdentityPresigner(fakePresigner{}),
	}
	client, err := eksclient.NewClientWithCredentialRequirement(
		t.Context(), "eu-central-1", false, base...,
	)
	require.NoError(t, err)
	assert.NotNil(t, client)
}

// assertTokenCredentialPrefix verifies the minted token was signed by the expected access key.
func assertTokenCredentialPrefix(t *testing.T, token, expected string) {
	t.Helper()

	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "k8s-aws-v1."))
	require.NoError(t, err)

	signedURL, err := url.Parse(string(decoded))
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(signedURL.Query().Get("X-Amz-Credential"), expected))
}
