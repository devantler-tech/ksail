package k8s_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

var (
	errFakeGetFailed    = errors.New("get failed")
	errFakeCreateFailed = errors.New("create failed")
	errFakeUpdateFailed = errors.New("update failed")
)

//nolint:funlen // Table-style subtests keep namespace label permutations explicit.
func TestEnsurePrivilegedNamespace(t *testing.T) {
	t.Parallel()

	pssLabelKeys := []string{
		"pod-security.kubernetes.io/enforce",
		"pod-security.kubernetes.io/audit",
		"pod-security.kubernetes.io/warn",
	}

	tests := []struct {
		name      string
		existing  []runtime.Object
		namespace string
		wantErr   bool
	}{
		{
			name:      "creates namespace when it does not exist",
			existing:  nil,
			namespace: "new-ns",
		},
		{
			name: "updates existing namespace without labels",
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "existing-ns",
					},
				},
			},
			namespace: "existing-ns",
		},
		{
			name: "updates existing namespace with nil labels",
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "nil-labels-ns",
						Labels: nil,
					},
				},
			},
			namespace: "nil-labels-ns",
		},
		{
			name: "no-op when labels already correct",
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "correct-ns",
						Labels: map[string]string{
							"pod-security.kubernetes.io/enforce": "privileged",
							"pod-security.kubernetes.io/audit":   "privileged",
							"pod-security.kubernetes.io/warn":    "privileged",
						},
					},
				},
			},
			namespace: "correct-ns",
		},
		{
			name: "updates existing namespace with partial labels",
			existing: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "partial-ns",
						Labels: map[string]string{
							"pod-security.kubernetes.io/enforce": "privileged",
							// missing audit and warn
						},
					},
				},
			},
			namespace: "partial-ns",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			clientset := k8sfake.NewClientset(testCase.existing...)

			err := k8s.EnsurePrivilegedNamespace(
				context.Background(),
				clientset,
				testCase.namespace,
			)

			if testCase.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			// Verify the namespace exists with correct labels
			namespace, getErr := clientset.CoreV1().Namespaces().Get(
				context.Background(),
				testCase.namespace,
				metav1.GetOptions{},
			)
			require.NoError(t, getErr)
			require.NotNil(t, namespace)

			for _, key := range pssLabelKeys {
				assert.Equal(t, "privileged", namespace.Labels[key],
					"expected label %s=privileged", key)
			}
		})
	}
}

func TestEnsurePrivilegedNamespace_GetError(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()
	clientset.PrependReactor(
		"get",
		"namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errFakeGetFailed
		},
	)

	err := k8s.EnsurePrivilegedNamespace(
		context.Background(),
		clientset,
		"test-ns",
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "get namespace")
}

func TestEnsurePrivilegedNamespace_CreateError(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset()

	// Allow get to return not-found (default), but fail on create
	clientset.PrependReactor(
		"create",
		"namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errFakeCreateFailed
		},
	)

	err := k8s.EnsurePrivilegedNamespace(
		context.Background(),
		clientset,
		"test-ns",
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create namespace")
}

func TestEnsurePrivilegedNamespace_UpdateError(t *testing.T) {
	t.Parallel()

	clientset := k8sfake.NewClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "update-fail-ns",
			// No PSS labels, so update will be triggered
		},
	})

	clientset.PrependReactor(
		"update",
		"namespaces",
		func(_ k8stesting.Action) (bool, runtime.Object, error) {
			return true, nil, errFakeUpdateFailed
		},
	)

	err := k8s.EnsurePrivilegedNamespace(
		context.Background(),
		clientset,
		"update-fail-ns",
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "update namespace labels")
}
