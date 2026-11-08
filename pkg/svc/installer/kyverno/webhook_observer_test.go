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
	clientset := k8sfake.NewClientset(
		webhookObservationFixture("kyverno-resource-mutating-webhook-cfg"),
	)
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

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookResourceTLSRemainsRequired(t *testing.T) {
	tests := map[string]*admissionregistrationv1.MutatingWebhookConfiguration{
		"missing resource configuration": nil,
		"resource CA not published": webhookObservationFixture(
			"kyverno-resource-mutating-webhook-cfg", nil,
		),
		"one resource CA is still empty": webhookObservationFixture(
			"kyverno-resource-mutating-webhook-cfg", []byte("first-resource-ca"), nil,
		),
	}
	for name, resource := range tests {
		t.Run(name, func(t *testing.T) {
			clientset := k8sfake.NewClientset(webhookObservationFixture(
				"kyverno-policy-mutating-webhook-cfg", []byte("policy-ca"),
			))
			if resource != nil {
				require.NoError(t, clientset.Tracker().Add(resource))
			}

			installer := webhookObserver(t, clientset)

			ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
			defer cancel()

			require.ErrorIs(t, installer.WaitForWebhookReadyForTest(ctx), context.DeadlineExceeded)
		})
	}
}

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookResourceWaitRechecksPolicyTLS(t *testing.T) {
	clientset := k8sfake.NewClientset()
	policyReads, resourceReads := 0, 0

	clientset.PrependReactor("get", "mutatingwebhookconfigurations",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			getAction, ok := action.(k8stesting.GetAction)
			if !ok {
				return false, nil, nil
			}

			switch getAction.GetName() {
			case "kyverno-policy-mutating-webhook-cfg":
				policyReads++

				var bundle []byte
				if policyReads == 1 {
					bundle = []byte("policy-ca")
				}

				return true, webhookObservationFixture(getAction.GetName(), bundle), nil
			case "kyverno-resource-mutating-webhook-cfg":
				resourceReads++

				var bundle []byte
				if resourceReads > 1 {
					bundle = []byte("resource-ca")
				}

				return true, webhookObservationFixture(getAction.GetName(), bundle), nil
			default:
				return false, nil, nil
			}
		},
	)
	installer := webhookObserver(t, clientset)

	ctx, cancel := context.WithTimeout(t.Context(), 2500*time.Millisecond)
	defer cancel()

	// Resource publication cannot finish against an earlier policy observation.
	require.ErrorIs(t, installer.WaitForWebhookReadyForTest(ctx), context.DeadlineExceeded)
}

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookResourceReadFailurePropagates(t *testing.T) {
	tests := map[string]*admissionregistrationv1.MutatingWebhookConfiguration{
		"ready policy": webhookObservationFixture(
			"kyverno-policy-mutating-webhook-cfg", []byte("policy-ca"),
		),
		"missing policy": nil,
		"empty policy entries": webhookObservationFixture(
			"kyverno-policy-mutating-webhook-cfg",
		),
		"unpublished policy CA": webhookObservationFixture(
			"kyverno-policy-mutating-webhook-cfg", nil,
		),
	}
	for name, policy := range tests {
		t.Run(name, func(t *testing.T) {
			clientset := k8sfake.NewClientset()
			if policy != nil {
				require.NoError(t, clientset.Tracker().Add(policy))
			}

			clientset.PrependReactor("get", "mutatingwebhookconfigurations",
				func(action k8stesting.Action) (bool, runtime.Object, error) {
					getAction, ok := action.(k8stesting.GetAction)
					if !ok || getAction.GetName() != "kyverno-resource-mutating-webhook-cfg" {
						return false, nil, nil
					}

					return true, nil, apierrors.NewForbidden(
						schema.GroupResource{
							Group:    "admissionregistration.k8s.io",
							Resource: "mutatingwebhookconfigurations",
						},
						getAction.GetName(), errConnRefused,
					)
				},
			)
			installer := webhookObserver(t, clientset)

			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()

			err := installer.WaitForWebhookReadyForTest(ctx)
			require.True(t, apierrors.IsForbidden(err),
				"expected forbidden resource read, got %v", err)
		})
	}
}

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookResourceCAEventuallyPublished(t *testing.T) {
	clientset := k8sfake.NewClientset(
		webhookObservationFixture("kyverno-policy-mutating-webhook-cfg", []byte("policy-ca")),
		webhookObservationFixture("kyverno-resource-mutating-webhook-cfg", nil),
	)
	resourceReads := 0

	clientset.PrependReactor("get", "mutatingwebhookconfigurations",
		func(action k8stesting.Action) (bool, runtime.Object, error) {
			getAction, ok := action.(k8stesting.GetAction)
			if !ok || getAction.GetName() != "kyverno-resource-mutating-webhook-cfg" {
				return false, nil, nil
			}

			resourceReads++
			if resourceReads > 1 {
				config := webhookObservationFixture(
					getAction.GetName(), []byte("published-resource-ca"),
				)

				updateErr := clientset.Tracker().Update(
					admissionregistrationv1.SchemeGroupVersion.WithResource("mutatingwebhookconfigurations"),
					config, "",
				)
				if updateErr != nil {
					return true, nil, fmt.Errorf("publishing resource CA: %w", updateErr)
				}
			}

			return false, nil, nil
		},
	)
	installer := webhookObserver(t, clientset)

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	require.NoError(t, installer.WaitForWebhookReadyForTest(ctx))

	observed, err := clientset.Tracker().Get(
		admissionregistrationv1.SchemeGroupVersion.WithResource("mutatingwebhookconfigurations"),
		"", "kyverno-resource-mutating-webhook-cfg",
	)
	require.NoError(t, err)

	config, ok := observed.(*admissionregistrationv1.MutatingWebhookConfiguration)
	require.True(t, ok)
	require.NotEmpty(t, config.Webhooks[0].ClientConfig.CABundle,
		"readiness returned before the resource CA was published")
}

//nolint:paralleltest // Overrides the shared clientset factory.
func TestWebhookReadinessAcceptsPublishedPolicyAndResourceCAs(t *testing.T) {
	clientset := k8sfake.NewClientset(
		webhookObservationFixture(
			"kyverno-policy-mutating-webhook-cfg", []byte("policy-ca-1"), []byte("policy-ca-2"),
		),
		webhookObservationFixture(
			"kyverno-resource-mutating-webhook-cfg",
			[]byte("resource-ca-1"), []byte("resource-ca-2"),
		),
	)
	installer := webhookObserver(t, clientset)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	require.NoError(t, installer.WaitForWebhookReadyForTest(ctx))
}
