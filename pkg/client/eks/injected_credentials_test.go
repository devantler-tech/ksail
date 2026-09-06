package eks_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInjectedClientsKeepExplicitCredentialInventory catches initialization that
// skips EKS or CloudFormation inventory when only the older seams are injected.
func TestInjectedClientsKeepExplicitCredentialInventory(t *testing.T) {
	for _, source := range []string{"static", "profile"} {
		t.Run(source, func(t *testing.T) {
			option, accessKey := injectedInventoryCredentials(t, source)
			calls := 0
			server := httptest.NewServer(injectedInventoryHandler(t, accessKey, &calls))
			t.Cleanup(server.Close)
			t.Setenv("AWS_ENDPOINT_URL", server.URL)
			t.Setenv("AWS_ENDPOINT_URL_EKS", server.URL)
			t.Setenv("AWS_ENDPOINT_URL_CLOUDFORMATION", server.URL)
			t.Setenv("AWS_IGNORE_CONFIGURED_ENDPOINT_URLS", "false")
			client, err := eksclient.NewClient(t.Context(), "us-east-1",
				eksclient.WithClusterDescriber(fakeDescriber{}),
				eksclient.WithCallerIdentityPresigner(fakePresigner{}),
				option,
			)
			require.NoError(t, err)
			groups, err := client.ListManagedNodegroups(t.Context(), "demo")
			require.NoError(t, err)
			assert.Empty(t, groups)
			exists, err := client.NodegroupStackExists(t.Context(), "demo", "workers")
			require.NoError(t, err)
			assert.False(t, exists)
			assert.Equal(t, 2, calls)
		})
	}
}

func injectedInventoryCredentials(t *testing.T, source string) (eksclient.Option, string) {
	t.Helper()
	dir := t.TempDir()
	credentialsFile := filepath.Join(dir, "credentials")
	configFile := filepath.Join(dir, "config")

	require.NoError(t, os.WriteFile(
		credentialsFile,
		[]byte(
			"[selected]\naws_access_key_id = PROFILEACCESS\naws_secret_access_key = profile-secret\n",
		),
		0o600,
	))
	require.NoError(t, os.WriteFile(configFile, nil, 0o600))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", credentialsFile)
	t.Setenv("AWS_CONFIG_FILE", configFile)
	t.Setenv("AWS_PROFILE", "missing-ambient-profile")
	t.Setenv("AWS_DEFAULT_PROFILE", "missing-ambient-profile")
	t.Setenv("AWS_ACCESS_KEY_ID", "AMBIENTACCESS")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "ambient-secret")
	t.Setenv("AWS_SESSION_TOKEN", "")

	if source == "profile" {
		return eksclient.WithCredentialValues("selected", "", "", ""), "PROFILEACCESS"
	}

	return eksclient.WithCredentialValues("", "STATICACCESS", "static-secret", ""), "STATICACCESS"
}

func injectedInventoryHandler(t *testing.T, accessKey string, calls *int) http.HandlerFunc {
	t.Helper()

	return func(writer http.ResponseWriter, request *http.Request) {
		(*calls)++

		assert.Contains(
			t,
			request.Header.Get("Authorization"),
			"Credential="+accessKey+"/",
		)
		assert.NotContains(t, request.Header.Get("Authorization"), "AMBIENTACCESS")

		if request.Method == http.MethodGet {
			assert.Equal(t, "/clusters/demo/node-groups", request.URL.Path)
			assert.Contains(
				t,
				request.Header.Get("Authorization"),
				"/us-east-1/eks/aws4_request",
			)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"nodegroups":[]}`))

			return
		}

		const maxFormBytes = 4096

		request.Body = http.MaxBytesReader(writer, request.Body, maxFormBytes)
		if !assert.NoError(t, request.ParseForm()) {
			return
		}

		assert.Equal(t, "ListStacks", request.PostForm.Get("Action"))
		assert.Contains(
			t,
			request.Header.Get("Authorization"),
			"/us-east-1/cloudformation/aws4_request",
		)
		writer.Header().Set("Content-Type", "text/xml")
		_, _ = writer.Write(
			[]byte(stackResponseStart + `<StackSummaries/>` + stackResponseEnd),
		)
	}
}
