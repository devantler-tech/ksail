package eks_test

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	awsconfigutil "github.com/devantler-tech/ksail/v7/pkg/awsconfig"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type endpointSnapshotCase struct {
	name                                  string
	initialGlobal, initialEKS, initialSTS string
	shared, ignored                       bool
	wantEKS, wantSTS                      string
}

// TestFrozenServiceEndpointsSurviveAmbientChanges uses real EKS and STS SDK requests.
//
//nolint:paralleltest // Helpers change process endpoint configuration using t.Setenv.
func TestFrozenServiceEndpointsSurviveAmbientChanges(t *testing.T) {
	// Not parallel: changes process endpoint configuration to exercise lazy SDK construction.
	for _, testCase := range []endpointSnapshotCase{
		{
			"changed_service", "", "https://eks.selected.test", "https://sts.selected.test",
			false, false, "eks.selected.test", "sts.selected.test",
		},
		{"unset_service", "", "", "", false, false, "eks.us-east-1.amazonaws.com", "sts.us-east-1.amazonaws.com"},
		{"shared_service", "", "", "", true, false, "eks.shared.test", "sts.shared.test"},
		{
			"global_precedence", "https://global.selected.test", "", "", true, false,
			"global.selected.test", "global.selected.test",
		},
		{
			"ignored_service", "", "https://eks.ignored.test", "https://sts.ignored.test",
			false, true, "eks.us-east-1.amazonaws.com", "sts.us-east-1.amazonaws.com",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			exerciseEndpointSnapshot(t, testCase)
		})
	}
}

func exerciseEndpointSnapshot(t *testing.T, testCase endpointSnapshotCase) {
	t.Helper()
	setEndpointVariable(t, "AWS_ENDPOINT_URL", testCase.initialGlobal)
	setEndpointVariable(t, "AWS_ENDPOINT_URL_EKS", testCase.initialEKS)
	setEndpointVariable(t, "AWS_ENDPOINT_URL_STS", testCase.initialSTS)

	cfg := endpointSnapshotConfig(testCase)

	calls := 0
	cfg.HTTPClient = endpointSnapshotHTTPFixture(
		t,
		testCase.wantEKS,
		testCase.wantSTS,
		&calls,
	)
	cfg = awsconfigutil.FreezeEndpointSources(cfg)
	// A newly present global variable plus removed service keys bypasses the SDK's
	// ConfigSources lookup unless the final frozen option is applied as well.
	t.Setenv("AWS_ENDPOINT_URL", "https://ambient.invalid")
	require.NoError(t, os.Unsetenv("AWS_ENDPOINT_URL_EKS"))
	require.NoError(t, os.Unsetenv("AWS_ENDPOINT_URL_STS"))
	client, err := eksclient.NewClient(
		t.Context(),
		"us-east-1",
		eksclient.WithAWSConfig(cfg),
	)
	require.NoError(t, err)
	_, err = client.DescribeCluster(t.Context(), "demo")
	require.NoError(t, err)
	_, err = client.CallerAccountID(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
	t.Setenv("AWS_ENDPOINT_URL_EKS", "https://later-eks.invalid")
	t.Setenv("AWS_ENDPOINT_URL_STS", "https://later-sts.invalid")
	client, err = eksclient.NewClient(
		t.Context(),
		"us-east-1",
		eksclient.WithAWSConfig(cfg),
	)
	require.NoError(t, err)
	_, err = client.DescribeCluster(t.Context(), "demo")
	require.NoError(t, err)
	_, err = client.CallerAccountID(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 4, calls)
}

func endpointSnapshotConfig(testCase endpointSnapshotCase) aws.Config {
	cfg := aws.Config{
		Region: "us-east-1",
		Credentials: awscredentials.NewStaticCredentialsProvider(
			"FROZENENDPOINT",
			"secret",
			"",
		),
		ConfigSources: []any{
			config.EnvConfig{
				Region:                    "us-east-1",
				IgnoreConfiguredEndpoints: aws.Bool(testCase.ignored),
			},
		},
	}
	if testCase.initialGlobal != "" {
		cfg.BaseEndpoint = aws.String(testCase.initialGlobal)
	}

	if testCase.shared {
		cfg.ConfigSources = append(
			cfg.ConfigSources,
			config.SharedConfig{Services: config.Services{
				ServiceValues: map[string]map[string]string{
					"eks": {"endpoint_url": "https://eks.shared.test"},
					"sts": {"endpoint_url": "https://sts.shared.test"},
				},
			}},
		)
	}

	return cfg
}

type frozenEndpointHTTPClient func(*http.Request) (*http.Response, error)

func (f frozenEndpointHTTPClient) Do(request *http.Request) (*http.Response, error) {
	return f(request)
}

func setEndpointVariable(t *testing.T, key, value string) {
	t.Helper()
	t.Setenv(key, value)

	if value == "" {
		require.NoError(t, os.Unsetenv(key))
	}
}

func endpointSnapshotHTTPFixture(
	t *testing.T,
	wantEKS, wantSTS string,
	calls *int,
) frozenEndpointHTTPClient {
	t.Helper()

	return func(request *http.Request) (*http.Response, error) {
		*calls++
		contentType, body := "application/json", `{"cluster":{"name":"demo"}}`

		want := wantEKS
		if request.URL.Path == "/" {
			want = wantSTS
			contentType = "text/xml"
			body = `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">` +
				`<GetCallerIdentityResult><Account>123456789012</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`
		}

		assert.Equal(t, want, request.URL.Host)
		assert.Equal(t, "https", request.URL.Scheme)
		assert.Contains(t, request.Header.Get("Authorization"), "Credential=FROZENENDPOINT/")

		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}},
			Body: io.NopCloser(strings.NewReader(body)), Request: request,
		}, nil
	}
}
