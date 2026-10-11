package eksprovisioner_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awseks "github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	eksclient "github.com/devantler-tech/ksail/v7/pkg/client/eks"
	"github.com/devantler-tech/ksail/v7/pkg/client/eksctl"
	eksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/eks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	upgradeFixtureSource = "1.34"
	upgradeFixtureTarget = "1.35"
)

type upgradeHTTPFixture struct {
	t         *testing.T
	submitted int
}

// Do serves the upgrade and convergence responses while observing actual signed
// SDK requests, so tests can prove whether any mutation was submitted.
func (f *upgradeHTTPFixture) Do(request *http.Request) (*http.Response, error) {
	f.t.Helper()
	assert.Contains(f.t, request.Header.Get("Authorization"), "Credential=selected/")

	var body string

	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/clusters/demo/updates":
		f.submitted++
		body = `{"update":{"id":"id","type":"VersionUpdate","status":"InProgress",` +
			`"params":[{"type":"Version","value":"1.35"}]}}`
	case request.URL.Path == "/clusters/demo/updates/id":
		body = `{"update":{"id":"id","type":"VersionUpdate","status":"Successful",` +
			`"params":[{"type":"Version","value":"1.35"}]}}`
	case request.URL.Path == "/clusters/demo":
		version := upgradeFixtureSource
		if f.submitted > 0 {
			version = upgradeFixtureTarget
		}

		body = fmt.Sprintf(
			`{"cluster":{"name":"demo","arn":"arn:aws:eks:us-east-1:123456789012:cluster/demo",`+
				`"createdAt":1700000000,"status":"ACTIVE","version":%q}}`,
			version,
		)
	default:
		f.t.Errorf("unexpected AWS request: %s %s", request.Method, request.URL.Path)
	}

	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: request,
	}, nil
}

// upgradeCredentialProvider returns a fixed synthetic session with its expiry intact.
func upgradeCredentialProvider(values aws.Credentials) aws.CredentialsProvider {
	return aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
		return values, nil
	})
}

// TestControlPlaneUpgradeChecksEffectiveSDKCredentials rejects unsafe EKS or STS
// service overrides before mutation while allowing sessions that cover the wait.
func TestControlPlaneUpgradeChecksEffectiveSDKCredentials(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name, service string
		lifetime      time.Duration
		temporary     bool
		wantErr       bool
	}{
		{name: "permanent"},
		{name: "eks_short", service: "EKS", temporary: true, lifetime: 30 * time.Minute, wantErr: true},
		{name: "sts_short", service: "STS", temporary: true, lifetime: 30 * time.Minute, wantErr: true},
		{name: "eks_unknown", service: "EKS", temporary: true, wantErr: true},
		{name: "sts_unknown", service: "STS", temporary: true, wantErr: true},
		{name: "expired", service: "EKS", temporary: true, lifetime: -time.Minute, wantErr: true},
		{name: "eks_sufficient", service: "EKS", temporary: true, lifetime: 2 * time.Hour},
		{name: "sts_sufficient", service: "STS", temporary: true, lifetime: 2 * time.Hour},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			transport := &upgradeHTTPFixture{t: t}

			values := aws.Credentials{AccessKeyID: "selected", SecretAccessKey: "secret"}
			if testCase.temporary {
				values.SessionToken = "session"
			}

			if testCase.lifetime != 0 {
				values.CanExpire = true
				values.Expires = time.Now().Add(testCase.lifetime)
			}

			cfg := upgradeSDKConfig(transport, testCase.service, values)
			provisioner := newSDKUpgradeProvisioner(t, cfg)

			err := provisioner.UpgradeKubernetes(
				t.Context(), "demo", upgradeFixtureSource, upgradeFixtureTarget,
			)
			if testCase.wantErr {
				require.ErrorContains(t, err, "credential")
				assert.Zero(
					t,
					transport.submitted,
					"unsafe credentials must never submit an upgrade",
				)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 1, transport.submitted)
			}
		})
	}
}

// opaqueUpgradeAPI deliberately hides the lifetime validator, modelling a custom
// implementation whose effective credentials cannot be verified.
type opaqueUpgradeAPI struct {
	eksprovisioner.AWSClusterVersionAPI
}

// TestControlPlaneUpgradeRejectsUnverifiableClient prevents a custom API from
// bypassing the credential-lifetime boundary by omitting its validator.
func TestControlPlaneUpgradeRejectsUnverifiableClient(t *testing.T) {
	t.Parallel()

	api := &upgradeAPI{cluster: upgradeCluster(), result: upgradeResult()}
	provisioner := newUpgradeProvisioner(t, api, func(context.Context) error { return nil },
		eksprovisioner.WithAWSClusterAPI(opaqueUpgradeAPI{AWSClusterVersionAPI: api}),
	)
	err := provisioner.UpgradeKubernetes(
		t.Context(),
		"demo",
		upgradeFixtureSource,
		upgradeFixtureTarget,
	)
	require.ErrorIs(t, err, eksclient.ErrUpgradeCredentialLifetime)
	assert.Zero(t, api.submitted)
}

// upgradeSDKConfig applies a service-specific provider over a valid base selection.
func upgradeSDKConfig(transport aws.HTTPClient, service string, values aws.Credentials) aws.Config {
	return aws.Config{
		Region:     "us-east-1",
		HTTPClient: transport,
		Credentials: upgradeCredentialProvider(
			aws.Credentials{AccessKeyID: "selected", SecretAccessKey: "secret"},
		),
		ServiceOptions: []func(string, any){func(_ string, options any) {
			switch value := options.(type) {
			case *awseks.Options:
				if service == "EKS" {
					value.Credentials = upgradeCredentialProvider(values)
				}
			case *sts.Options:
				if service == "STS" {
					value.Credentials = upgradeCredentialProvider(values)
				}
			}
		}},
	}
}

// newSDKUpgradeProvisioner exercises the real EKS client with an in-memory AWS transport.
func newSDKUpgradeProvisioner(t *testing.T, cfg aws.Config) *eksprovisioner.UpgradableProvisioner {
	t.Helper()
	client, err := eksclient.NewClient(
		t.Context(),
		"us-east-1",
		eksclient.WithAWSConfig(cfg),
	)
	require.NoError(t, err)
	base, err := eksprovisioner.NewProvisioner(
		"demo",
		"us-east-1",
		"",
		eksctl.NewClient(),
		nil,
		eksprovisioner.WithAWSClusterAPI(client),
		eksprovisioner.WithAWSConfig(cfg),
		eksprovisioner.WithOwnershipVerifier(func(context.Context) error { return nil }),
	)
	require.NoError(t, err)

	return eksprovisioner.NewUpgradableProvisioner(
		eksprovisioner.NewUpdatableProvisioner(base),
	)
}

// signedRequest records one AWS request and the access key that signed it.
type signedRequest struct{ operation, key string }

// signerRecordingFixture serves the upgrade like upgradeHTTPFixture while recording the
// signing key of every request in order.
type signerRecordingFixture struct {
	mu        sync.Mutex
	submitted int
	requests  []signedRequest
}

func (f *signerRecordingFixture) Do(request *http.Request) (*http.Response, error) {
	_, credential, _ := strings.Cut(request.Header.Get("Authorization"), "Credential=")
	key, _, _ := strings.Cut(credential, "/")

	f.mu.Lock()
	defer f.mu.Unlock()

	var body string

	switch {
	case request.Method == http.MethodPost && request.URL.Path == "/clusters/demo/updates":
		f.submitted++
		body = `{"update":{"id":"id","type":"VersionUpdate","status":"InProgress",` +
			`"params":[{"type":"Version","value":"1.35"}]}}`
	case request.URL.Path == "/clusters/demo/updates/id":
		body = `{"update":{"id":"id","type":"VersionUpdate","status":"Successful",` +
			`"params":[{"type":"Version","value":"1.35"}]}}`
	default:
		version := upgradeFixtureSource
		if f.submitted > 0 {
			version = upgradeFixtureTarget
		}

		body = fmt.Sprintf(
			`{"cluster":{"name":"demo","arn":"arn:aws:eks:us-east-1:123456789012:cluster/demo",`+
				`"createdAt":1700000000,"status":"ACTIVE","version":%q}}`,
			version,
		)
	}

	f.requests = append(
		f.requests,
		signedRequest{operation: request.Method + " " + request.URL.Path, key: key},
	)

	return &http.Response{
		StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(body)), Request: request,
	}, nil
}

// TestControlPlaneUpgradeScopesValidatedCredentials binds the last pre-mutation cluster read to the
// credentials that sign the update, and returns the reused client to its refreshing provider after.
func TestControlPlaneUpgradeScopesValidatedCredentials(t *testing.T) {
	t.Parallel()

	var retrieved atomic.Int64

	transport := &signerRecordingFixture{}
	cfg := aws.Config{
		Region:     "us-east-1",
		HTTPClient: transport,
		Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID: fmt.Sprintf("AKID%d", retrieved.Add(1)), SecretAccessKey: "secret",
			}, nil
		}),
	}
	client, err := eksclient.NewClient(t.Context(), "us-east-1", eksclient.WithAWSConfig(cfg))
	require.NoError(t, err)

	base, err := eksprovisioner.NewProvisioner(
		"demo", "us-east-1", "", eksctl.NewClient(), nil,
		eksprovisioner.WithAWSClusterAPI(client),
		eksprovisioner.WithAWSConfig(cfg),
		eksprovisioner.WithOwnershipVerifier(func(context.Context) error { return nil }),
	)
	require.NoError(t, err)

	provisioner := eksprovisioner.NewUpgradableProvisioner(
		eksprovisioner.NewUpdatableProvisioner(base),
	)
	require.NoError(
		t,
		provisioner.UpgradeKubernetes(
			t.Context(),
			"demo",
			upgradeFixtureSource,
			upgradeFixtureTarget,
		),
	)

	_, err = client.DescribeCluster(t.Context(), "demo")
	require.NoError(t, err)

	requests := transport.requests
	submission := slices.IndexFunc(requests, func(request signedRequest) bool {
		return request.operation == "POST /clusters/demo/updates"
	})
	require.Positive(t, submission)
	assert.Equal(t, "GET /clusters/demo", requests[submission-1].operation)
	assert.Equal(t, requests[submission].key, requests[submission-1].key,
		"the last cluster read before submission must use the update's signer")
	assert.NotEqual(t, requests[submission].key, requests[len(requests)-1].key,
		"the upgrade's credentials must not outlive the upgrade")
}
