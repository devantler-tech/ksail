package kyvernopolicy_test

import (
	"errors"
	"net"
	"sync/atomic"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// networkContextPolicy references every context source that can reach the network: a service
// apiCall (the Kyverno SSRF advisories), a Kubernetes apiCall and a ConfigMap (cross-namespace
// reads), and an image registry lookup (registry credential helpers).
//
// Each source sits in its own rule: variable substitution stops at the first entry that fails to
// resolve, so one rule referencing all four would only ever reach the first.
const networkContextPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: network-context
spec:
  validationFailureAction: Enforce
  rules:
  - name: service-call
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    context:
    - name: service
      apiCall:
        method: GET
        service:
          url: http://%[1]s/ssrf
    validate:
      message: "service context must stay unloaded"
      deny:
        conditions:
          any:
          - key: "{{ service }}"
            operator: Equals
            value: "unreachable"
  - name: kubernetes-call
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    context:
    - name: kube
      apiCall:
        urlPath: /api/v1/namespaces/kube-system/secrets
    validate:
      message: "kubernetes context must stay unloaded"
      deny:
        conditions:
          any:
          - key: "{{ kube }}"
            operator: Equals
            value: "unreachable"
  - name: configmap
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    context:
    - name: settings
      configMap:
        name: settings
        namespace: kube-system
    validate:
      message: "configmap context must stay unloaded"
      deny:
        conditions:
          any:
          - key: "{{ settings }}"
            operator: Equals
            value: "unreachable"
  - name: image-registry
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    context:
    - name: image
      imageRegistry:
        reference: %[1]s/app:v1
        imageRegistryCredentials:
          allowInsecureRegistry: true
    validate:
      message: "image registry context must stay unloaded"
      deny:
        conditions:
          any:
          - key: "{{ image }}"
            operator: Equals
            value: "unreachable"
`

// networkContextRules is the number of rules in networkContextPolicy.
const networkContextRules = 4

// countConnections accepts TCP connections on a loopback listener and counts them, so a request of
// any protocol (plain HTTP, TLS, registry) is observed, not only well-formed HTTP.
func countConnections(t *testing.T) (string, *atomic.Int64) {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	var connections atomic.Int64

	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if errors.Is(acceptErr, net.ErrClosed) {
				return
			}

			if acceptErr == nil {
				connections.Add(1)

				_ = conn.Close()
			}
		}
	}()

	t.Cleanup(func() { _ = listener.Close() })

	return listener.Addr().String(), &connections
}

// TestEvaluate_NetworkContextIsNeverLoaded pins that the offline evaluator opens no connection for a
// policy's context entries. The engine has no Kubernetes client, registry client, ConfigMap resolver
// or global context store, so Kyverno disables every network-backed loader; this is why the Kyverno
// and credential-helper advisories in .govulncheck-allow.txt are not exploitable through ksail.
func TestEvaluate_NetworkContextIsNeverLoaded(t *testing.T) {
	t.Parallel()

	address, connections := countConnections(t)
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{
		policy(t, sprintf(networkContextPolicy, address)),
	})

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(
		t,
		violations,
		networkContextRules,
		"every rule must run, or its context was never reached",
	)
	assert.Zero(t, connections.Load(), "evaluating a policy must not open a network connection")
}
