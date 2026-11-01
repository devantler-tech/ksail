package kyvernoinstaller_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	kyvernoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/kyverno"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookReadinessWithoutResourcePolicies(t *testing.T) {
	clientset := k8sfake.NewClientset(
		webhookObservationFixture("kyverno-resource-mutating-webhook-cfg"),
		webhookObservationFixture("kyverno-policy-mutating-webhook-cfg", []byte("policy-ca")),
	)
	installer := webhookObserver(t, clientset)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	// A policy-free resource configuration may stay empty indefinitely.
	// The always-registered policy webhook must still establish TLS readiness.
	require.NoError(t, installer.WaitForWebhookReadyForTest(ctx))
}

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookReadinessRequiresPolicyTLS(t *testing.T) {
	tests := map[string]*admissionregistrationv1.MutatingWebhookConfiguration{
		"missing policy configuration": nil,
		"empty policy webhook list": webhookObservationFixture(
			"kyverno-policy-mutating-webhook-cfg",
		),
		"empty policy CA": webhookObservationFixture(
			"kyverno-policy-mutating-webhook-cfg", nil,
		),
		"one policy CA is still empty": webhookObservationFixture(
			"kyverno-policy-mutating-webhook-cfg", []byte("first-policy-ca"), nil,
		),
	}

	for name, policy := range tests {
		t.Run(name, func(t *testing.T) {
			clientset := k8sfake.NewClientset(webhookObservationFixture(
				"kyverno-resource-mutating-webhook-cfg", []byte("resource-ca"),
			))
			if policy != nil {
				require.NoError(t, clientset.Tracker().Add(policy))
			}

			installer := webhookObserver(t, clientset)

			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()

			// Ready resource webhooks cannot substitute for missing policy TLS.
			require.ErrorIs(t, installer.WaitForWebhookReadyForTest(ctx), context.DeadlineExceeded)
		})
	}
}

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookReadinessWaitsForPolicyCA(t *testing.T) {
	clientset := k8sfake.NewClientset()
	policyReads := 0

	clientset.PrependReactor("get", "mutatingwebhookconfigurations",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			getAction, ok := action.(k8stesting.GetAction)
			if !ok || getAction.GetName() != "kyverno-policy-mutating-webhook-cfg" {
				return false, nil, nil
			}

			policyReads++

			var caBundle []byte
			if policyReads > 1 {
				caBundle = []byte("published-policy-ca")
			}

			return true, webhookObservationFixture(
				"kyverno-policy-mutating-webhook-cfg", caBundle,
			), nil
		},
	)
	installer := webhookObserver(t, clientset)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	require.NoError(t, installer.WaitForWebhookReadyForTest(ctx))
}

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookReadinessPropagatesPolicyReadFailure(t *testing.T) {
	clientset := k8sfake.NewClientset()
	clientset.PrependReactor("get", "mutatingwebhookconfigurations",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, apierrors.NewForbidden(
				schema.GroupResource{
					Group:    "admissionregistration.k8s.io",
					Resource: "mutatingwebhookconfigurations",
				},
				"kyverno-policy-mutating-webhook-cfg",
				errConnRefused,
			)
		},
	)
	installer := webhookObserver(t, clientset)

	err := installer.WaitForWebhookReadyForTest(t.Context())
	require.True(t, apierrors.IsForbidden(err), "expected forbidden API error, got %v", err)
}

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookReadinessPreservesCancellation(t *testing.T) {
	clientset := k8sfake.NewClientset(webhookObservationFixture(
		"kyverno-policy-mutating-webhook-cfg", nil,
	))
	installer := webhookObserver(t, clientset)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, installer.WaitForWebhookReadyForTest(ctx), context.Canceled)
}

func webhookObserver(t *testing.T, clientset kubernetes.Interface) *kyvernoinstaller.Installer {
	t.Helper()
	t.Cleanup(kyvernoinstaller.SetNewClientsetFn(
		func(_, _ string) (kubernetes.Interface, error) { return clientset, nil },
	))
	installer, _ := newInstallerWithDefaults(t)

	return installer
}

func webhookObservationFixture(
	name string,
	bundles ...[]byte,
) *admissionregistrationv1.MutatingWebhookConfiguration {
	config := &admissionregistrationv1.MutatingWebhookConfiguration{Name: name}
	for index, bundle := range bundles {
		config.Webhooks = append(config.Webhooks, admissionregistrationv1.MutatingWebhook{
			Name: fmt.Sprintf("hook-%d.kyverno.svc", index),
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				CABundle: bundle,
			},
		})
	}

	return config
}
