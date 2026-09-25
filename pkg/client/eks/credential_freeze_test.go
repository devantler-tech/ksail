package eks_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/stretchr/testify/require"
)

// signingRecorder answers EKS and STS requests in memory and records which access key signed each.
type signingRecorder struct {
	mu   sync.Mutex
	keys map[string][]string
}

func (r *signingRecorder) Do(request *http.Request) (*http.Response, error) {
	auth := request.Header.Get("Authorization")
	_, credential, _ := strings.Cut(auth, "Credential=")
	key, _, _ := strings.Cut(credential, "/")

	operation, body := "UpdateClusterVersion", `{"update":{"id":"update-1","status":"InProgress"}}`
	if strings.Contains(request.URL.Host, "sts") {
		operation = "GetCallerIdentity"
		body = `<GetCallerIdentityResponse><GetCallerIdentityResult>` +
			`<Account>123456789012</Account></GetCallerIdentityResult></GetCallerIdentityResponse>`
	}

	r.mu.Lock()
	r.keys[operation] = append(r.keys[operation], key)
	r.mu.Unlock()

	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}, nil
}

// TestUpgradeSignsWithValidatedCredentials catches a refreshing provider handing the
// irreversible upgrade and later identity checks credentials other than the ones whose
// lifetime was validated.
func TestUpgradeSignsWithValidatedCredentials(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(10*time.Minute))
	defer cancel()

	recorder := &signingRecorder{keys: map[string][]string{}}
	client := newRetrievalCountingClient(ctx, t, recorder)

	require.NoError(t, client.ValidateUpgradeCredentialLifetime(ctx))

	_, err := client.UpdateClusterVersion(ctx, "cluster", "1.34", "token")
	require.NoError(t, err)

	_, err = client.CallerAccountID(ctx)
	require.NoError(t, err)

	require.Equal(t, []string{"AKIDRETRIEVAL1"}, recorder.keys["UpdateClusterVersion"])
	require.Equal(t, []string{"AKIDRETRIEVAL2"}, recorder.keys["GetCallerIdentity"])
}

// newRetrievalCountingClient returns a client whose provider yields a new access key on every
// retrieval, so a test can tell which retrieval signed each request.
func newRetrievalCountingClient(
	ctx context.Context, t *testing.T, recorder *signingRecorder,
) *eksclient.Client {
	t.Helper()

	var (
		retrievalLock sync.Mutex
		retrieved     int
	)

	client, err := eksclient.NewClient(ctx, "us-east-1", eksclient.WithAWSConfig(aws.Config{
		Region:     "us-east-1",
		HTTPClient: recorder,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			retrievalLock.Lock()
			defer retrievalLock.Unlock()

			retrieved++

			return aws.Credentials{
				AccessKeyID: fmt.Sprintf("AKIDRETRIEVAL%d", retrieved), SecretAccessKey: "secret",
			}, nil
		}),
	}))
	require.NoError(t, err)

	return client
}

// TestMintTokenSignsWithValidatedCredentials catches a token minted during an upgrade
// carrying an identity other than the STS credentials whose lifetime was validated.
func TestMintTokenSignsWithValidatedCredentials(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(10*time.Minute))
	defer cancel()

	client := newRetrievalCountingClient(ctx, t, &signingRecorder{keys: map[string][]string{}})

	require.NoError(t, client.ValidateUpgradeCredentialLifetime(ctx))

	token, err := client.MintToken(ctx, "cluster")
	require.NoError(t, err)

	assertTokenCredentialPrefix(t, token, "AKIDRETRIEVAL2/")
}

// TestReleasedUpgradeCredentialsReturnToProvider catches a reused client signing with an
// upgrade's credentials after that upgrade ends, when the session may be past its checked lifetime.
func TestReleasedUpgradeCredentialsReturnToProvider(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(10*time.Minute))
	defer cancel()

	recorder := &signingRecorder{keys: map[string][]string{}}
	client := newRetrievalCountingClient(ctx, t, recorder)

	require.NoError(t, client.ValidateUpgradeCredentialLifetime(ctx))
	client.ReleaseUpgradeCredentials()

	_, err := client.UpdateClusterVersion(ctx, "cluster", "1.34", "token")
	require.NoError(t, err)

	_, err = client.CallerAccountID(ctx)
	require.NoError(t, err)

	require.Equal(t, []string{"AKIDRETRIEVAL3"}, recorder.keys["UpdateClusterVersion"])
	require.Equal(t, []string{"AKIDRETRIEVAL4"}, recorder.keys["GetCallerIdentity"])
}

// TestUpgradeCredentialsRefuseOverlappingUpgrade catches a second upgrade replacing the
// credentials an earlier upgrade on the same client still signs with.
func TestUpgradeCredentialsRefuseOverlappingUpgrade(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(10*time.Minute))
	defer cancel()

	recorder := &signingRecorder{keys: map[string][]string{}}
	client := newRetrievalCountingClient(ctx, t, recorder)

	require.NoError(t, client.ValidateUpgradeCredentialLifetime(ctx))
	require.ErrorIs(
		t, client.ValidateUpgradeCredentialLifetime(ctx), eksclient.ErrUpgradeCredentialsInUse,
	)

	_, err := client.UpdateClusterVersion(ctx, "cluster", "1.34", "token")
	require.NoError(t, err)
	require.Equal(t, []string{"AKIDRETRIEVAL1"}, recorder.keys["UpdateClusterVersion"])

	client.ReleaseUpgradeCredentials()
	require.NoError(t, client.ValidateUpgradeCredentialLifetime(ctx))
}
