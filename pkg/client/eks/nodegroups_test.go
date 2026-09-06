package eks_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newNodegroupHTTPClient(
	t *testing.T, handler http.HandlerFunc, options ...eksclient.Option,
) *eksclient.Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	options = append(options, eksclient.WithAWSConfig(aws.Config{
		Region:       "stale-region",
		BaseEndpoint: aws.String(server.URL),
		HTTPClient:   server.Client(),
		Credentials: credentials.NewStaticCredentialsProvider(
			"FROZENACCESS",
			"test-secret",
			"",
		),
		RetryMaxAttempts: 1,
	}))
	client, err := eksclient.NewClient(t.Context(), "us-east-1", options...)
	require.NoError(t, err)

	return client
}

func TestListManagedNodegroupsFollowsEveryPageAndPreservesFrozenCredentials(t *testing.T) {
	t.Parallel()

	var pages, descriptions int

	client := newNodegroupHTTPClient(t, func(writer http.ResponseWriter, request *http.Request) {
		assert.Contains(t, request.Header.Get("Authorization"), "Credential=FROZENACCESS/")
		assert.Contains(t, request.Header.Get("Authorization"), "/us-east-1/eks/aws4_request")
		writer.Header().Set("Content-Type", "application/json")

		switch request.URL.Path {
		case "/clusters/demo/node-groups":
			pages++

			if request.URL.Query().Get("nextToken") == "second-page" {
				_, _ = writer.Write([]byte(`{"nodegroups":["second"]}`))
			} else {
				_, _ = writer.Write([]byte(`{"nodegroups":["first"],"nextToken":"second-page"}`))
			}
		default:
			descriptions++
			name := strings.TrimPrefix(request.URL.Path, "/clusters/demo/node-groups/")
			_ = json.NewEncoder(writer).Encode(map[string]any{"nodegroup": map[string]any{
				"clusterName": "demo", "nodegroupName": name, "status": "ACTIVE",
				"tags": map[string]string{"ksail.io/nodegroup-creation-id": "test-marker"},
			}})
		}
	})
	groups, err := client.ListManagedNodegroups(t.Context(), "demo")
	require.NoError(t, err)
	require.Len(t, groups, 2)
	assert.Equal(t, 2, pages)
	assert.Equal(t, 2, descriptions)
	assert.Equal(t, "second", aws.ToString(groups[1].NodegroupName))
	assert.Equal(t, "test-marker", groups[1].Tags["ksail.io/nodegroup-creation-id"])
}

func TestListManagedNodegroupsDoesNotReturnPartialInventory(t *testing.T) {
	t.Parallel()

	for _, scenario := range []string{
		"page failure", "repeated token", "duplicate name", "missing describe", "wrong identity",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			client := newNodegroupHTTPClient(t, nodegroupFailureHandler(scenario))
			groups, err := client.ListManagedNodegroups(t.Context(), "demo")
			require.Error(t, err)
			assert.Nil(t, groups)
		})
	}
}

func nodegroupFailureHandler(scenario string) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")

		if strings.HasSuffix(request.URL.Path, "/node-groups") {
			if scenario == "page failure" && request.URL.Query().Get("nextToken") != "" {
				writer.WriteHeader(http.StatusForbidden)

				return
			}

			if scenario == "repeated token" {
				_, _ = writer.Write([]byte(`{"nodegroups":[],"nextToken":"again"}`))

				return
			}

			_, _ = writer.Write([]byte(`{"nodegroups":["first"],"nextToken":"again"}`))

			return
		}

		if scenario == "missing describe" {
			_, _ = writer.Write([]byte(`{}`))

			return
		}

		clusterName := "demo"
		if scenario == "wrong identity" {
			clusterName = "other"
		}

		_, _ = writer.Write(
			[]byte(`{"nodegroup":{"clusterName":"` + clusterName + `","nodegroupName":"first"}}`),
		)
	}
}

func TestListManagedNodegroupsEmptyInventoryIsAnExplicitSlice(t *testing.T) {
	t.Parallel()
	client := newNodegroupHTTPClient(t, func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"nodegroups":[]}`))
	})
	groups, err := client.ListManagedNodegroups(t.Context(), "demo")
	require.NoError(t, err)
	assert.NotNil(t, groups)
	assert.Empty(t, groups)
}

func TestListManagedNodegroupsMissingNamesAreUnknown(t *testing.T) {
	t.Parallel()

	for _, malformed := range []string{`{}`, `{"nodegroups":null}`} {
		t.Run(malformed, func(t *testing.T) {
			t.Parallel()

			for _, laterPage := range []bool{false, true} {
				client := newNodegroupHTTPClient(
					t,
					func(writer http.ResponseWriter, request *http.Request) {
						writer.Header().Set("Content-Type", "application/json")

						if laterPage && request.URL.Query().Get("nextToken") == "" {
							_, _ = writer.Write([]byte(`{"nodegroups":[],"nextToken":"later"}`))

							return
						}

						_, _ = writer.Write([]byte(malformed))
					},
				)
				groups, err := client.ListManagedNodegroups(t.Context(), "demo")
				require.Error(t, err)
				assert.Nil(t, groups)
			}
		})
	}
}

// TestInjectedClientsKeepExplicitInventoryConfiguration checks that optional seams
// do not suppress frozen EKS and CloudFormation inventory client construction.
//

func TestInjectedClientsKeepExplicitInventoryConfiguration(t *testing.T) {
	t.Setenv("AWS_PROFILE", "missing-injected-profile")
	t.Setenv("AWS_ACCESS_KEY_ID", "AMBIENTACCESS")

	calls := 0
	client := newNodegroupHTTPClient(t, func(writer http.ResponseWriter, request *http.Request) {
		calls++

		assert.Contains(t, request.Header.Get("Authorization"), "Credential=FROZENACCESS/")

		if request.Method == http.MethodGet {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"nodegroups":[]}`))

			return
		}

		writer.Header().Set("Content-Type", "text/xml")
		_, _ = writer.Write([]byte(stackResponseStart + `<StackSummaries/>` + stackResponseEnd))
	}, eksclient.WithClusterDescriber(fakeDescriber{}), eksclient.WithCallerIdentityPresigner(fakePresigner{}))
	_, err := client.ListManagedNodegroups(t.Context(), "demo")
	require.NoError(t, err)
	exists, err := client.NodegroupStackExists(t.Context(), "demo", "workers")
	require.NoError(t, err)
	assert.False(t, exists)
	assert.Equal(t, 2, calls)
	_, err = eksclient.NewClient(
		t.Context(),
		"us-east-1",
		eksclient.WithClusterDescriber(
			fakeDescriber{},
		),
		eksclient.WithCallerIdentityPresigner(fakePresigner{}),
	)
	require.NoError(t, err, "injected-only construction must not load ambient configuration")
}
