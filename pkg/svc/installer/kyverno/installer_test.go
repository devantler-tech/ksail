package kyvernoinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	kyvernoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/kyverno"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := kyvernoinstaller.NewInstaller(client, 5*time.Second, "", "")

	assert.NotNil(t, installer)
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestInstallSuccess(t *testing.T) {
	// Not parallel: overrides the package-level newClientsetFn var.
	fakeClientset := k8sfake.NewClientset(readyWebhookConfig())

	restore := kyvernoinstaller.SetNewClientsetFn(
		func(_, _ string) (kubernetes.Interface, error) { return fakeClientset, nil },
	)
	defer restore()

	installer, client := newInstallerWithDefaults(t)
	expectKyvernoInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstallRepoError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().
		GetReleaseSecretLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add kyverno repository")
}

func TestInstallChartError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	expectKyvernoInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install kyverno chart")
}

func TestUninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().UninstallRelease(mock.Anything, "kyverno", "kyverno").Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestUninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "kyverno", "kyverno").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall kyverno release")
}

func newInstallerWithDefaults(
	t *testing.T,
) (*kyvernoinstaller.Installer, *helm.MockInterface) {
	t.Helper()

	client := helm.NewMockInterface(t)
	installer := kyvernoinstaller.NewInstaller(client, 2*time.Minute, "", "")

	return installer, client
}

func expectKyvernoInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()

	client.EXPECT().
		GetReleaseSecretLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				return entry != nil && entry.Name == "kyverno" &&
					entry.URL == "https://kyverno.github.io/kyverno/"
			}),
			mock.Anything,
		).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				if spec == nil {
					return false
				}

				assert.Equal(t, "kyverno", spec.ReleaseName)
				assert.Equal(t, "kyverno/kyverno", spec.ChartName)
				assert.Equal(t, "kyverno", spec.Namespace)
				assert.Equal(t, "https://kyverno.github.io/kyverno/", spec.RepoURL)
				assert.True(t, spec.CreateNamespace)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)
				assert.Equal(t, 2*time.Minute, spec.Timeout)

				return true
			}),
		).
		Return(nil, installErr)
}

// readyWebhookConfig returns a MutatingWebhookConfiguration with a populated
// caBundle, simulating a fully-initialised Kyverno admission controller.
func readyWebhookConfig() *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "kyverno-resource-mutating-webhook-cfg"},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name: "mutate.kyverno.svc",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					CABundle: []byte("fake-ca-bundle"),
				},
			},
		},
	}
}

// unreadyWebhookConfig returns a MutatingWebhookConfiguration with an empty
// caBundle, simulating a Kyverno admission controller that has not yet
// initialised its TLS certificate.
func unreadyWebhookConfig() *admissionregistrationv1.MutatingWebhookConfiguration {
	return &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "kyverno-resource-mutating-webhook-cfg"},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			{
				Name:         "mutate.kyverno.svc",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{},
			},
		},
	}
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestInstallWebhookDelayedReadiness(t *testing.T) {
	// Not parallel: overrides the package-level newClientsetFn var.
	fakeClientset := k8sfake.NewClientset(unreadyWebhookConfig())

	restore := kyvernoinstaller.SetNewClientsetFn(
		func(_, _ string) (kubernetes.Interface, error) { return fakeClientset, nil },
	)
	defer restore()

	// Populate caBundle after a short delay to simulate delayed TLS init.
	// Use a buffered channel so the goroutine never blocks if Install returns first.
	updateErrCh := make(chan error, 1)

	go func() {
		time.Sleep(500 * time.Millisecond)

		cfg := readyWebhookConfig()

		_, err := fakeClientset.AdmissionregistrationV1().
			MutatingWebhookConfigurations().
			Update(context.Background(), cfg, metav1.UpdateOptions{})
		updateErrCh <- err
	}()

	installer, client := newInstallerWithDefaults(t)
	expectKyvernoInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
	require.NoError(t, <-updateErrCh, "webhook Update in goroutine failed")
}

//nolint:paralleltest // Mutates shared test seams exposed by export_test.go.
func TestInstallWebhookTimeoutIsNonBlocking(t *testing.T) {
	// Not parallel: overrides the package-level newClientsetFn and
	// webhookReadinessTimeout vars.
	fakeClientset := k8sfake.NewClientset(unreadyWebhookConfig())

	restoreClientset := kyvernoinstaller.SetNewClientsetFn(
		func(_, _ string) (kubernetes.Interface, error) { return fakeClientset, nil },
	)
	defer restoreClientset()

	// Use a very short webhook timeout so the test does not wait 5 minutes.
	restoreTimeout := kyvernoinstaller.SetWebhookReadinessTimeout(500 * time.Millisecond)
	defer restoreTimeout()

	installer, client := newInstallerWithDefaults(t)
	expectKyvernoInstall(t, client, nil)

	// The caBundle is never populated, but Install should return nil because the
	// webhook wait is best-effort — failurePolicy: Ignore ensures API operations
	// succeed even without a fully initialised webhook.
	err := installer.Install(context.Background())

	require.NoError(t, err)
}
