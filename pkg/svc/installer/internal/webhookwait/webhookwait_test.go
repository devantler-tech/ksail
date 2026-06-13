package webhookwait_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/webhookwait"
	"github.com/stretchr/testify/require"
	admv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// mutatingConfig builds a MutatingWebhookConfiguration with one webhook entry
// carrying the given caBundle (pass nil for an unready, empty caBundle).
func mutatingConfig(name string, caBundle []byte) *admv1.MutatingWebhookConfiguration {
	return &admv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Webhooks: []admv1.MutatingWebhook{
			{Name: "a", ClientConfig: admv1.WebhookClientConfig{CABundle: caBundle}},
		},
	}
}

// validatingConfig builds a ValidatingWebhookConfiguration with one webhook
// entry carrying the given caBundle (pass nil for an unready, empty caBundle).
func validatingConfig(name string, caBundle []byte) *admv1.ValidatingWebhookConfiguration {
	return &admv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Webhooks: []admv1.ValidatingWebhook{
			{Name: "a", ClientConfig: admv1.WebhookClientConfig{CABundle: caBundle}},
		},
	}
}

func TestPollMutatingImmediatelyReady(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(mutatingConfig("mwc", []byte("ca")))

	err := webhookwait.Poll(
		context.Background(),
		clientset,
		webhookwait.Mutating,
		"mwc",
		"polling mwc",
		5*time.Second,
	)

	require.NoError(t, err)
}

func TestPollValidatingImmediatelyReady(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(validatingConfig("vwc", []byte("ca")))

	err := webhookwait.Poll(
		context.Background(),
		clientset,
		webhookwait.Validating,
		"vwc",
		"polling vwc",
		5*time.Second,
	)

	require.NoError(t, err)
}

func TestPollValidatingNotFoundThenReady(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()

	createErrCh := make(chan error, 1)

	go func() {
		time.Sleep(50 * time.Millisecond)

		_, err := clientset.AdmissionregistrationV1().
			ValidatingWebhookConfigurations().
			Create(context.Background(), validatingConfig("vwc", []byte("ca")), metav1.CreateOptions{})
		createErrCh <- err
	}()

	err := webhookwait.Poll(
		context.Background(),
		clientset,
		webhookwait.Validating,
		"vwc",
		"polling vwc",
		3*time.Second,
	)

	require.NoError(t, err)
	require.NoError(t, <-createErrCh, "webhook Create in goroutine failed")
}

func TestPollEmptyCABundleTimesOut(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(mutatingConfig("mwc", nil))

	err := webhookwait.Poll(
		context.Background(),
		clientset,
		webhookwait.Mutating,
		"mwc",
		"polling mwc",
		200*time.Millisecond,
	)

	require.Error(t, err)
	require.Contains(t, err.Error(), "polling mwc")
}
