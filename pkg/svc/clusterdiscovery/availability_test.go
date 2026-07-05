package clusterdiscovery_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mapResolver resolves credentials from a fixed map, using default env var names.
type mapResolver map[credentials.Key]string

func (m mapResolver) Value(key credentials.Key) string  { return m[key] }
func (m mapResolver) EnvVar(key credentials.Key) string { return credentials.DefaultEnvVar(key) }

func pingOK(context.Context) error  { return nil }
func pingErr(context.Context) error { return errBoom }

func lookPathFound(string) (string, error) { return "/usr/local/bin/eksctl", nil }

func availabilityFor(
	avails []clusterdiscovery.Availability,
	prov v1alpha1.Provider,
) clusterdiscovery.Availability {
	for _, a := range avails {
		if a.Provider == prov {
			return a
		}
	}

	return clusterdiscovery.Availability{}
}

func TestAvailability_DockerReflectsPing(t *testing.T) {
	t.Parallel()

	up := (&clusterdiscovery.Discoverer{DockerPing: pingOK}).
		Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderDocker})
	assert.True(t, up[0].Available)

	down := (&clusterdiscovery.Discoverer{DockerPing: pingErr}).
		Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderDocker})
	assert.False(t, down[0].Available)
	assert.NotEmpty(t, down[0].Reason)
}

func TestAvailability_HetznerRequiresToken(t *testing.T) {
	t.Parallel()

	withToken := &clusterdiscovery.Discoverer{
		Resolver: mapResolver{credentials.HetznerToken: "tok"},
	}
	got := availabilityFor(
		withToken.Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderHetzner}),
		v1alpha1.ProviderHetzner,
	)
	assert.True(t, got.Available)

	withoutToken := &clusterdiscovery.Discoverer{Resolver: mapResolver{}}
	got = availabilityFor(
		withoutToken.Availability(
			context.Background(),
			[]v1alpha1.Provider{v1alpha1.ProviderHetzner},
		),
		v1alpha1.ProviderHetzner,
	)
	assert.False(t, got.Available)
	assert.Contains(t, got.Reason, "HCLOUD_TOKEN")
}

func TestAvailability_OmniRequiresBothCredentials(t *testing.T) {
	t.Parallel()

	// Only the endpoint set: still unavailable, and the reason names the missing key.
	partial := &clusterdiscovery.Discoverer{
		Resolver: mapResolver{credentials.OmniEndpoint: "https://omni.example"},
	}
	got := availabilityFor(
		partial.Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderOmni}),
		v1alpha1.ProviderOmni,
	)
	assert.False(t, got.Available)
	assert.Contains(t, got.Reason, "OMNI_SERVICE_ACCOUNT_KEY")

	both := &clusterdiscovery.Discoverer{
		Resolver: mapResolver{
			credentials.OmniEndpoint:          "https://omni.example",
			credentials.OmniServiceAccountKey: "key",
		},
	}
	got = availabilityFor(
		both.Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderOmni}),
		v1alpha1.ProviderOmni,
	)
	assert.True(t, got.Available)
}

func TestAvailability_AWSRequiresEksctl(t *testing.T) {
	t.Parallel()

	// eksctl missing => unavailable regardless of credentials.
	noEksctl := &clusterdiscovery.Discoverer{
		LookPath: lookPathMissing,
		Resolver: mapResolver{credentials.AWSAccessKeyID: "AKIA..."},
	}
	got := availabilityFor(
		noEksctl.Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderAWS}),
		v1alpha1.ProviderAWS,
	)
	assert.False(t, got.Available)
	assert.Contains(t, got.Reason, "eksctl")

	// eksctl present + a complete pair of static credentials => available.
	ready := &clusterdiscovery.Discoverer{
		LookPath: lookPathFound,
		Resolver: mapResolver{
			credentials.AWSAccessKeyID:     "AKIA...",
			credentials.AWSSecretAccessKey: "secret...",
		},
	}
	got = availabilityFor(
		ready.Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderAWS}),
		v1alpha1.ProviderAWS,
	)
	assert.True(t, got.Available)
}

// TestAvailability_AWSRequiresBothStaticKeys verifies a lone access key ID (no secret, no profile,
// no shared ~/.aws files) does not mark AWS available. HOME is pointed at an empty temp dir so the
// shared-config check is deterministic.
func TestAvailability_AWSRequiresBothStaticKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	lookPath := lookPathFound

	idOnly := availabilityFor(
		(&clusterdiscovery.Discoverer{
			LookPath: lookPath,
			Resolver: mapResolver{credentials.AWSAccessKeyID: "AKIA..."},
		}).Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderAWS}),
		v1alpha1.ProviderAWS,
	)
	assert.False(t, idOnly.Available, "access key ID alone must not enable AWS")

	bothKeys := availabilityFor(
		(&clusterdiscovery.Discoverer{
			LookPath: lookPath,
			Resolver: mapResolver{
				credentials.AWSAccessKeyID:     "AKIA...",
				credentials.AWSSecretAccessKey: "secret...",
			},
		}).Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderAWS}),
		v1alpha1.ProviderAWS,
	)
	assert.True(t, bothKeys.Available, "a complete static key pair must enable AWS")
}

func TestAvailability_GCPRequiresProject(t *testing.T) {
	t.Parallel()

	// The project check short-circuits before any host credential-file probe, so this stays
	// deterministic regardless of the machine's real gcloud setup.
	got := availabilityFor(
		(&clusterdiscovery.Discoverer{Resolver: mapResolver{}}).
			Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderGCP}),
		v1alpha1.ProviderGCP,
	)
	assert.False(t, got.Available)
	assert.Contains(t, got.Reason, "GOOGLE_CLOUD_PROJECT")
}

// TestAvailability_GCPRequiresADC verifies a project alone (no GOOGLE_APPLICATION_CREDENTIALS, no
// gcloud well-known ADC file) does not mark GCP available. HOME is pointed at an empty temp dir so
// the well-known-file check is deterministic.
func TestAvailability_GCPRequiresADC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")

	projectOnly := availabilityFor(
		(&clusterdiscovery.Discoverer{
			Resolver: mapResolver{credentials.GCPProject: "my-project"},
		}).Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderGCP}),
		v1alpha1.ProviderGCP,
	)
	assert.False(t, projectOnly.Available, "a project without ADC must not enable GCP")
	assert.Contains(t, projectOnly.Reason, "Application Default Credentials")

	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", filepath.Join(t.TempDir(), "adc.json"))

	withADC := availabilityFor(
		(&clusterdiscovery.Discoverer{
			Resolver: mapResolver{credentials.GCPProject: "my-project"},
		}).Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderGCP}),
		v1alpha1.ProviderGCP,
	)
	assert.True(t, withADC.Available, "a project plus ADC must enable GCP")
}

// TestAvailability_GCPWellKnownADCFile verifies the gcloud well-known ADC file alone (no
// GOOGLE_APPLICATION_CREDENTIALS variable) satisfies the credential probe.
func TestAvailability_GCPWellKnownADCFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "")

	adcPath := filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(adcPath), 0o750))
	require.NoError(t, os.WriteFile(adcPath, []byte("{}"), 0o600))

	got := availabilityFor(
		(&clusterdiscovery.Discoverer{
			Resolver: mapResolver{credentials.GCPProject: "my-project"},
		}).Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderGCP}),
		v1alpha1.ProviderGCP,
	)
	assert.True(t, got.Available, "the gcloud well-known ADC file must enable GCP")
}

func TestAvailability_KubernetesRequiresHostKubeconfig(t *testing.T) {
	t.Setenv("KSAIL_HOST_KUBECONFIG", "/definitely/not/a/real/kubeconfig")

	got := availabilityFor(
		(&clusterdiscovery.Discoverer{}).
			Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderKubernetes}),
		v1alpha1.ProviderKubernetes,
	)
	assert.False(t, got.Available)

	kubeconfig := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600))
	t.Setenv("KSAIL_HOST_KUBECONFIG", kubeconfig)

	got = availabilityFor(
		(&clusterdiscovery.Discoverer{}).
			Availability(context.Background(), []v1alpha1.Provider{v1alpha1.ProviderKubernetes}),
		v1alpha1.ProviderKubernetes,
	)
	assert.True(t, got.Available)
}
