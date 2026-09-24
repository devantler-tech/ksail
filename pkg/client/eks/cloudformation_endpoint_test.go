package eks_test

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudformation"
	awsconfigutil "github.com/devantler-tech/ksail/v7/pkg/awsconfig"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCloudFormationUsesCapturedEndpoint(t *testing.T) {
	for _, testCase := range []struct {
		name, global, service, shared, explicit, want string
		ignored                                       bool
	}{
		{name: "service_removed", service: "https://selected.test", want: "selected.test"},
		{name: "default_retained", want: "cloudformation.us-east-1.amazonaws.com"},
		{name: "shared_retained", shared: "https://shared.test", want: "shared.test"},
		{name: "global_retained", global: "https://global.test", shared: "https://shared.test", want: "global.test"},
		{
			name: "ignored_retained", service: "https://ignored.test", ignored: true,
			want: "cloudformation.us-east-1.amazonaws.com",
		},
		{name: "explicit_wins", service: "https://selected.test", explicit: "https://explicit.test", want: "explicit.test"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			setEndpointVariable(t, "AWS_ENDPOINT_URL", testCase.global)
			setEndpointVariable(t, "AWS_ENDPOINT_URL_CLOUDFORMATION", testCase.service)

			cfg := endpointSnapshotConfig(endpointSnapshotCase{
				initialGlobal: testCase.global, ignored: testCase.ignored,
			})
			if testCase.shared != "" {
				cfg.ConfigSources = append(
					cfg.ConfigSources,
					config.SharedConfig{Services: config.Services{
						ServiceValues: map[string]map[string]string{
							"cloudformation": {"endpoint_url": testCase.shared},
						},
					}},
				)
			}

			if testCase.explicit != "" {
				cfg.ServiceOptions = []func(string, any){func(_ string, options any) {
					if value, ok := options.(*cloudformation.Options); ok {
						value.BaseEndpoint = aws.String(testCase.explicit)
					}
				}}
			}

			calls := 0
			cfg.HTTPClient = cloudFormationSnapshotHTTPFixture(t, testCase.want, &calls)
			cfg = awsconfigutil.FreezeEndpointSources(cfg)

			t.Setenv("AWS_ENDPOINT_URL", "https://ambient.invalid")
			require.NoError(t, os.Unsetenv("AWS_ENDPOINT_URL_CLOUDFORMATION"))
			client, err := eksclient.NewClient(
				t.Context(),
				"us-east-1",
				eksclient.WithAWSConfig(cfg),
			)
			require.NoError(t, err)
			exists, err := client.NodegroupStackExists(t.Context(), "demo", "workers")
			require.NoError(t, err)
			assert.True(t, exists)
			assert.Equal(t, 1, calls)
		})
	}
}

// cloudFormationSnapshotHTTPFixture observes real signed SDK requests without network access.
func cloudFormationSnapshotHTTPFixture(
	t *testing.T,
	want string,
	calls *int,
) frozenEndpointHTTPClient {
	t.Helper()

	return func(request *http.Request) (*http.Response, error) {
		*calls++

		assert.Equal(t, want, request.URL.Host)
		assert.Contains(t, request.Header.Get("Authorization"), "Credential=FROZENENDPOINT/")

		body := stackResponseStart + "<StackSummaries><member>" +
			"<StackName>eksctl-demo-nodegroup-workers</StackName>" +
			"<StackStatus>CREATE_COMPLETE</StackStatus>" +
			"</member></StackSummaries>" + stackResponseEnd

		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/xml"}},
			Body: io.NopCloser(strings.NewReader(body)), Request: request,
		}, nil
	}
}
