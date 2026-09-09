package eks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscredentials "github.com/aws/aws-sdk-go-v2/credentials"
	ekstypes "github.com/aws/aws-sdk-go-v2/service/eks/types"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControlPlaneVersionStep(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct{ current, target, want string }{
		{"1.34", "1.35", "v1.35.0"},
		{"v1.34.0", " v1.35.0 ", "v1.35.0"},
		{"1.35", "1.35", "v1.35.0"},
		{"1.34", "1.36", ""},
		{"1.35", "1.34", ""},
		{"1.35", "2.0", ""},
		{"1.34", "1.35.1", ""},
		{"1.34", "1.35-rc.1", ""},
		{"1.34", "1.35.0+build", ""},
		{"", "1.35", ""},
		{"1.034", "1.35", ""},
		{"1.34", "", ""},
	} {
		t.Run(testCase.current+"/"+testCase.target, func(t *testing.T) {
			t.Parallel()

			got, err := eksclient.PlanControlPlaneUpgrade(testCase.current, testCase.target)
			if testCase.want == "" {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestControlPlaneUpdateUsesFrozenSDKAndExactRequest(t *testing.T) {
	t.Parallel()

	var calls []string

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			calls = append(calls, request.Method+" "+request.URL.Path)
			assert.Contains(t, request.Header.Get("Authorization"), "Credential=FROZENACCESS/")
			assert.Contains(t, request.Header.Get("Authorization"), "/us-east-1/eks/aws4_request")
			writer.Header().Set("Content-Type", "application/json")

			if request.Method == http.MethodPost {
				var body map[string]any

				request.Body = http.MaxBytesReader(writer, request.Body, 1024)
				assert.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				assert.Equal(
					t,
					map[string]any{"version": "1.35", "clientRequestToken": "operation-token"},
					body,
				)
			}

			_, _ = writer.Write(
				[]byte(
					`{"update":{"id":"update-id","type":"VersionUpdate","status":"Successful",` +
						`"params":[{"type":"Version","value":"1.35"}]}}`,
				),
			)
		}),
	)
	t.Cleanup(server.Close)
	client, err := eksclient.NewClient(t.Context(), "us-east-1", eksclient.WithAWSConfig(aws.Config{
		Region:       "us-east-1",
		BaseEndpoint: aws.String(server.URL),
		HTTPClient:   server.Client(),
		Credentials: awscredentials.NewStaticCredentialsProvider(
			"FROZENACCESS",
			"secret",
			"session",
		),
	}))
	require.NoError(t, err)
	update, err := client.UpdateClusterVersion(t.Context(), "demo", "1.35", "operation-token")
	require.NoError(t, err)
	assert.Equal(t, "update-id", aws.ToString(update.Id))
	update, err = client.DescribeClusterUpdate(t.Context(), "demo", "update-id")
	require.NoError(t, err)
	assert.Equal(t, ekstypes.UpdateStatusSuccessful, update.Status)
	assert.Equal(
		t,
		[]string{"POST /clusters/demo/updates", "GET /clusters/demo/updates/update-id"},
		calls,
	)
}

func TestControlPlaneUpdateRejectsMissingPayloadAndCancellation(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(
		http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte("{}"))
		}),
	)
	t.Cleanup(server.Close)
	client, err := eksclient.NewClient(t.Context(), "us-east-1", eksclient.WithAWSConfig(aws.Config{
		BaseEndpoint: aws.String(server.URL), HTTPClient: server.Client(),
		Credentials: awscredentials.NewStaticCredentialsProvider("KEY", "secret", ""),
	}))
	require.NoError(t, err)
	_, err = client.UpdateClusterVersion(t.Context(), "demo", "1.35", "token")
	require.ErrorIs(t, err, eksclient.ErrInvalidClusterUpdate)
	_, err = client.DescribeClusterUpdate(t.Context(), "demo", "id")
	require.ErrorIs(t, err, eksclient.ErrInvalidClusterUpdate)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = client.UpdateClusterVersion(ctx, "demo", "1.35", "token")
	require.ErrorIs(t, err, context.Canceled)
}
