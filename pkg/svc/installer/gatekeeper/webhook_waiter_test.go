package gatekeeperinstaller_test

import (
	"context"
	"testing"
	"time"

	gatekeeperinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/gatekeeper"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

func TestWaitForGatekeeperWebhookReadyImmediatelyReady(t *testing.T) {
	t.Parallel()

	fakeClientset := k8sfake.NewClientset(readyGatekeeperWebhookConfig())

	err := gatekeeperinstaller.WaitForGatekeeperWebhookReady(
		context.Background(), fakeClientset, 5*time.Second,
	)

	require.NoError(t, err)
}

func TestWaitForGatekeeperWebhookReadyNotFoundThenReady(t *testing.T) {
	t.Parallel()

	// Start with no webhook config — simulates the brief window between Helm
	// reporting install success and the chart actually creating the resource.
	fakeClientset := k8sfake.NewClientset()

	// Create the webhook config after a short delay, mimicking async chart creation.
	// The channel is buffered so the goroutine never blocks if the test returns early.
	createErrCh := make(chan error, 1)

	go func() {
		time.Sleep(500 * time.Millisecond)

		cfg := readyGatekeeperWebhookConfig()

		_, err := fakeClientset.AdmissionregistrationV1().
			ValidatingWebhookConfigurations().
			Create(context.Background(), cfg, metav1.CreateOptions{})
		createErrCh <- err
	}()

	err := gatekeeperinstaller.WaitForGatekeeperWebhookReady(
		context.Background(), fakeClientset, 10*time.Second,
	)

	require.NoError(t, err)
	require.NoError(t, <-createErrCh, "webhook Create in goroutine failed")
}

func TestWaitForGatekeeperWebhookReadyCaBundlePopulatedAfterDelay(t *testing.T) {
	t.Parallel()

	// Start with an empty caBundle — simulates the cert-controller not yet having
	// injected the TLS certificate into the ValidatingWebhookConfiguration.
	fakeClientset := k8sfake.NewClientset(unreadyGatekeeperWebhookConfig())

	// Populate caBundle after a short delay to simulate the cert-controller injection.
	// The channel is buffered so the goroutine never blocks if the test returns early.
	updateErrCh := make(chan error, 1)

	go func() {
		time.Sleep(500 * time.Millisecond)

		cfg := readyGatekeeperWebhookConfig()

		_, err := fakeClientset.AdmissionregistrationV1().
			ValidatingWebhookConfigurations().
			Update(context.Background(), cfg, metav1.UpdateOptions{})
		updateErrCh <- err
	}()

	err := gatekeeperinstaller.WaitForGatekeeperWebhookReady(
		context.Background(), fakeClientset, 10*time.Second,
	)

	require.NoError(t, err)
	require.NoError(t, <-updateErrCh, "webhook Update in goroutine failed")
}

// readyGatekeeperWebhookConfig returns a ValidatingWebhookConfiguration with a
// populated caBundle, simulating a fully-initialised Gatekeeper admission controller.
func readyGatekeeperWebhookConfig() *admissionregistrationv1.ValidatingWebhookConfiguration {
	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-validating-webhook-configuration"},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name: "validation.gatekeeper.sh",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{
					CABundle: []byte("fake-ca-bundle"),
				},
			},
		},
	}
}

// unreadyGatekeeperWebhookConfig returns a ValidatingWebhookConfiguration with an
// empty caBundle, simulating a Gatekeeper admission controller that has not yet
// initialised its TLS certificate.
func unreadyGatekeeperWebhookConfig() *admissionregistrationv1.ValidatingWebhookConfiguration {
	return &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: "gatekeeper-validating-webhook-configuration"},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			{
				Name:         "validation.gatekeeper.sh",
				ClientConfig: admissionregistrationv1.WebhookClientConfig{},
			},
		},
	}
}
