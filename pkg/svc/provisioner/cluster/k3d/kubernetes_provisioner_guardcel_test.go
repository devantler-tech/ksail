package k3dprovisioner_test

import (
	"context"
	"fmt"
	"maps"
	"testing"

	"cel.dev/cel-go/cel"
	k3dprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/k3d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"
)

// The guard's real behaviour lives in two CEL expressions, so asserting substrings of them only
// proves they were spelled a certain way. These tests compile and evaluate the expressions the
// provisioner actually ships and check the admit/reject decision for concrete pods.

const (
	guardTestCluster   = "nested-k3s"
	guardTestNamespace = "k3k-nested-k3s"
	guardServerPodName = "k3k-nested-k3s-server-0"
	controllerUsername = "system:serviceaccount:kube-system:statefulset-controller"
	// kubeControllerManagerUsername is the identity kube-controller-manager presents when it runs
	// without --use-service-account-credentials, where every built-in controller shares one
	// identity instead of getting a per-controller service account.
	kubeControllerManagerUsername = "system:kube-controller-manager"
	// hostileServiceAccountUsername is a service account any user able to create a pod in the
	// namespace can obtain a token for. It satisfies the class-level startsWith check while not
	// being the StatefulSet controller.
	hostileServiceAccountUsername = "system:serviceaccount:k3k-nested-k3s:default"
	regularUserUsername           = "kubernetes-admin"
)

// guardExpressions returns the isUnsafePod variable expression and the validation expression from
// a policy the provisioner built, so the tests evaluate shipped strings rather than copies.
func guardExpressions(t *testing.T) (string, string) {
	t.Helper()

	clientset := k8sfake.NewClientset()
	provisioner, err := k3dprovisioner.NewK3kProvisioner(k3dprovisioner.K3kProvisionerConfig{
		HostClientset: clientset,
		ClusterName:   guardTestCluster,
	})
	require.NoError(t, err)

	err = provisioner.EnsureNamespaceForTest(context.Background(), "", guardTestNamespace)
	require.NoError(t, err)

	policy, err := clientset.AdmissionregistrationV1().ValidatingAdmissionPolicies().Get(
		context.Background(), "ksail-k3k-nested-k3s-pod-security", metav1.GetOptions{},
	)
	require.NoError(t, err)
	require.Len(t, policy.Spec.Variables, 1)
	require.Len(t, policy.Spec.Validations, 1)

	return policy.Spec.Variables[0].Expression, policy.Spec.Validations[0].Expression
}

func guardEnv(t *testing.T) *cel.Env {
	t.Helper()

	env, err := cel.NewEnv(
		cel.Variable("object", cel.DynType),
		cel.Variable("request", cel.DynType),
		cel.Variable("variables", cel.DynType),
	)
	require.NoError(t, err)

	return env
}

func evalGuardExpr(t *testing.T, env *cel.Env, expr string, input map[string]any) (bool, error) {
	t.Helper()

	ast, issues := env.Compile(expr)
	require.NoError(t, issues.Err(), "expression must compile: %s", expr)

	program, err := env.Program(ast)
	require.NoError(t, err)

	out, _, err := program.Eval(input)
	if err != nil {
		return false, fmt.Errorf("evaluate guard expression: %w", err)
	}

	result, ok := out.Value().(bool)
	require.True(t, ok, "expression must evaluate to a bool")

	return result, nil
}

// pod builds a pod object carrying the fields the expressions dereference unguarded. A real
// AdmissionReview object is a typed Pod where these have zero values, so they are always present;
// a map fixture must supply them for the same reason.
func pod(overrides map[string]any) map[string]any {
	spec := map[string]any{
		"hostPID":     false,
		"hostIPC":     false,
		"hostNetwork": false,
		"containers":  []any{map[string]any{"name": "app"}},
	}
	maps.Copy(spec, overrides)

	return map[string]any{
		"metadata": map[string]any{
			"name":   guardServerPodName,
			"labels": map[string]any{"cluster": guardTestCluster, "role": "server"},
		},
		"spec": spec,
	}
}

func privilegedContainer(name string) any {
	return map[string]any{
		"name":            name,
		"securityContext": map[string]any{"privileged": true},
	}
}

type unsafePodCase struct {
	name   string
	spec   map[string]any
	unsafe bool
}

func unsafePodCases() []unsafePodCase {
	return append(hostLevelPodCases(), containerLevelPodCases()...)
}

// hostLevelPodCases cover the pod-scoped settings that break namespace isolation.
func hostLevelPodCases() []unsafePodCase {
	return []unsafePodCase{
		{name: "plain pod is safe", spec: nil, unsafe: false},
		{name: "hostPID", spec: map[string]any{"hostPID": true}, unsafe: true},
		{name: "hostIPC", spec: map[string]any{"hostIPC": true}, unsafe: true},
		{name: "hostNetwork", spec: map[string]any{"hostNetwork": true}, unsafe: true},
		{
			name: "hostPath volume",
			spec: map[string]any{"volumes": []any{
				map[string]any{"name": "h", "hostPath": map[string]any{"path": "/"}},
			}},
			unsafe: true,
		},
		{
			name: "unsafe sysctl",
			spec: map[string]any{"securityContext": map[string]any{
				"sysctls": []any{map[string]any{"name": "kernel.msgmax", "value": "65536"}},
			}},
			unsafe: true,
		},
		{
			name: "baseline sysctl",
			spec: map[string]any{"securityContext": map[string]any{
				"sysctls": []any{
					map[string]any{"name": "net.ipv4.tcp_syncookies", "value": "1"},
				},
			}},
			unsafe: false,
		},
	}
}

// containerLevelPodCases cover the per-container settings the baseline profile forbids, across
// all three container lists.
func containerLevelPodCases() []unsafePodCase {
	return []unsafePodCase{
		{
			name:   "privileged container",
			spec:   map[string]any{"containers": []any{privilegedContainer("app")}},
			unsafe: true,
		},
		{
			name:   "privileged init container",
			spec:   map[string]any{"initContainers": []any{privilegedContainer("init")}},
			unsafe: true,
		},
		{
			name:   "privileged ephemeral container",
			spec:   map[string]any{"ephemeralContainers": []any{privilegedContainer("debug")}},
			unsafe: true,
		},
		{
			name: "added capability beyond baseline",
			spec: map[string]any{"containers": []any{map[string]any{
				"name": "app",
				"securityContext": map[string]any{
					"capabilities": map[string]any{"add": []any{"SYS_ADMIN"}},
				},
			}}},
			unsafe: true,
		},
		{
			name: "NET_BIND_SERVICE alone is baseline-permitted",
			spec: map[string]any{"containers": []any{map[string]any{
				"name": "app",
				"securityContext": map[string]any{
					"capabilities": map[string]any{"add": []any{"NET_BIND_SERVICE"}},
				},
			}}},
			unsafe: false,
		},
		{
			name: "host port",
			spec: map[string]any{"containers": []any{map[string]any{
				"name":  "app",
				"ports": []any{map[string]any{"containerPort": int64(80), "hostPort": int64(80)}},
			}}},
			unsafe: true,
		},
		{
			name: "container port without host port",
			spec: map[string]any{"containers": []any{map[string]any{
				"name":  "app",
				"ports": []any{map[string]any{"containerPort": int64(80)}},
			}}},
			unsafe: false,
		},
	}
}

func TestGuardCEL_UnsafePodDetection(t *testing.T) {
	t.Parallel()

	unsafeExpr, _ := guardExpressions(t)
	env := guardEnv(t)

	for _, testCase := range unsafePodCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := evalGuardExpr(t, env, unsafeExpr, map[string]any{
				"object": pod(testCase.spec),
			})
			require.NoError(t, err)
			assert.Equal(t, testCase.unsafe, got)
		})
	}
}

type exemptionCase struct {
	name     string
	object   map[string]any
	username string
	admitted bool
}

// unsafePodNamed builds a privileged pod with the given name and labels. Passing nil labels omits
// the labels map entirely, which is what an unguarded CEL lookup trips over.
func unsafePodNamed(t *testing.T, name string, labels map[string]any) map[string]any {
	t.Helper()

	object := pod(map[string]any{"containers": []any{privilegedContainer("server")}})
	meta, ok := object["metadata"].(map[string]any)
	require.True(t, ok)

	meta["name"] = name

	if labels == nil {
		delete(meta, "labels")
	} else {
		meta["labels"] = labels
	}

	return object
}

func exemptionCases(t *testing.T) []exemptionCase {
	t.Helper()

	serverLabels := map[string]any{"cluster": guardTestCluster, "role": "server"}

	return []exemptionCase{
		{
			// The StatefulSet controller shares one identity with every other built-in
			// controller when --use-service-account-credentials is off, so the guard admits
			// that spelling too rather than letting the host distribution decide whether
			// k3k provisions at all.
			name:     "server pod created by a shared kube-controller-manager identity is admitted",
			object:   unsafePodNamed(t, guardServerPodName, serverLabels),
			username: kubeControllerManagerUsername,
			admitted: true,
		},
		{
			// The residual #6261 left behind: the class-level startsWith check proves only
			// that some service account made the request, so any service account holding
			// pod-create in this namespace reached the privileged exemption.
			name:     "non-controller service account spoofing the server pod is rejected",
			object:   unsafePodNamed(t, guardServerPodName, serverLabels),
			username: hostileServiceAccountUsername,
			admitted: false,
		},
		{
			name:     "controller-created server pod is admitted",
			object:   unsafePodNamed(t, guardServerPodName, serverLabels),
			username: controllerUsername,
			admitted: true,
		},
		{
			// The pod name and labels are attacker-controlled, so before the userInfo
			// conjunct this exact object was admitted.
			name:     "user-created pod spoofing the server name and labels is rejected",
			object:   unsafePodNamed(t, guardServerPodName, serverLabels),
			username: regularUserUsername,
			admitted: false,
		},
		{
			name: "wrong cluster label is rejected",
			object: unsafePodNamed(
				t,
				guardServerPodName,
				map[string]any{"cluster": "other", "role": "server"},
			),
			username: controllerUsername,
			admitted: false,
		},
		{
			name: "wrong role label is rejected",
			object: unsafePodNamed(
				t,
				guardServerPodName,
				map[string]any{"cluster": guardTestCluster, "role": "agent"},
			),
			username: controllerUsername,
			admitted: false,
		},
		{
			name:     "wrong name prefix is rejected",
			object:   unsafePodNamed(t, "attacker-pod", serverLabels),
			username: controllerUsername,
			admitted: false,
		},
	}
}

func TestGuardCEL_ExemptionAdmitsOnlyTheManagedServerPod(t *testing.T) {
	t.Parallel()

	_, validationExpr := guardExpressions(t)
	env := guardEnv(t)

	for _, testCase := range exemptionCases(t) {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := evalGuardExpr(t, env, validationExpr, map[string]any{
				"object": testCase.object,
				"request": map[string]any{
					"userInfo": map[string]any{"username": testCase.username},
				},
				"variables": map[string]any{"isUnsafePod": true},
			})
			require.NoError(t, err)
			assert.Equal(t, testCase.admitted, got)
		})
	}
}

// A safe pod never reaches the identity and label checks, so an ordinary workload keeps working
// regardless of who creates it.
func TestGuardCEL_SafePodShortCircuitsTheExemption(t *testing.T) {
	t.Parallel()

	_, validationExpr := guardExpressions(t)
	env := guardEnv(t)

	object := pod(nil)
	meta, ok := object["metadata"].(map[string]any)
	require.True(t, ok)

	meta["name"] = "some-workload"
	delete(meta, "labels")

	got, err := evalGuardExpr(t, env, validationExpr, map[string]any{
		"object":    object,
		"request":   map[string]any{"userInfo": map[string]any{"username": regularUserUsername}},
		"variables": map[string]any{"isUnsafePod": false},
	})
	require.NoError(t, err)
	assert.True(t, got)
}

// Missing-label regression. CEL raises "no such key" on an absent map key, and FailurePolicy is
// Fail, so an unguarded lookup turns every unsafe unlabelled pod into an evaluation error instead
// of a clean rejection carrying the guard's message. The negative control pins that the previous
// expression really did error, so this test fails if the key guards are removed.
func TestGuardCEL_UnlabelledUnsafePodIsRejectedNotErrored(t *testing.T) {
	t.Parallel()

	_, validationExpr := guardExpressions(t)
	env := guardEnv(t)

	object := pod(map[string]any{"containers": []any{privilegedContainer("app")}})
	meta, ok := object["metadata"].(map[string]any)
	require.True(t, ok)

	meta["name"] = guardServerPodName
	delete(meta, "labels")

	input := map[string]any{
		"object":    object,
		"request":   map[string]any{"userInfo": map[string]any{"username": controllerUsername}},
		"variables": map[string]any{"isUnsafePod": true},
	}

	got, err := evalGuardExpr(t, env, validationExpr, input)
	require.NoError(t, err, "shipped expression must not error on a pod carrying no labels")
	assert.False(t, got, "an unlabelled unsafe pod must be rejected")

	// Negative control: the unguarded form this replaced.
	unguarded := fmt.Sprintf(
		"!variables.isUnsafePod || (object.metadata.name.startsWith('%s') && "+
			"object.metadata.labels['cluster'] == '%s' && "+
			"object.metadata.labels['role'] == 'server')",
		"k3k-"+guardTestCluster+"-server-",
		guardTestCluster,
	)

	_, err = evalGuardExpr(t, env, unguarded, input)
	require.Error(t, err, "control must reproduce the missing-key failure the guards prevent")
	assert.Contains(t, err.Error(), "no such key")
}

// Identity-narrowing regression. #6261 proved only that *a* service account made the request, so
// every service account holding pod-create in the namespace reached the privileged exemption. The
// negative control pins that the class-level form really did admit a hostile service account, so
// this test fails if the exemption is ever widened back to a startsWith check.
func TestGuardCEL_ExemptionPinsTheStatefulSetControllerIdentity(t *testing.T) {
	t.Parallel()

	_, validationExpr := guardExpressions(t)
	env := guardEnv(t)

	input := map[string]any{
		"object": unsafePodNamed(
			t,
			guardServerPodName,
			map[string]any{"cluster": guardTestCluster, "role": "server"},
		),
		"request": map[string]any{
			"userInfo": map[string]any{"username": hostileServiceAccountUsername},
		},
		"variables": map[string]any{"isUnsafePod": true},
	}

	got, err := evalGuardExpr(t, env, validationExpr, input)
	require.NoError(t, err)
	assert.False(t, got, "a non-controller service account must not reach the exemption")

	// Negative control: the class-level form this replaced admitted the same pod.
	classLevel := fmt.Sprintf(
		"!variables.isUnsafePod || "+
			"(request.userInfo.username.startsWith('system:serviceaccount:') && "+
			"object.metadata.name.startsWith('%s') && "+
			"has(object.metadata.labels) && "+
			"'cluster' in object.metadata.labels && "+
			"object.metadata.labels['cluster'] == '%s' && "+
			"'role' in object.metadata.labels && "+
			"object.metadata.labels['role'] == 'server')",
		"k3k-"+guardTestCluster+"-server-",
		guardTestCluster,
	)

	admittedByControl, err := evalGuardExpr(t, env, classLevel, input)
	require.NoError(t, err)
	assert.True(
		t,
		admittedByControl,
		"control must reproduce the residual the narrowed identity check closes",
	)
}
