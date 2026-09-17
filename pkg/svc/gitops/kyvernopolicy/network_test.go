package kyvernopolicy_test

import (
	"errors"
	"net"
	"testing"
	"time"

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

// countConnections accepts TCP connections on a loopback listener, so a request of any protocol
// (plain HTTP, TLS, registry) is observed, not only well-formed HTTP. The returned function reports
// how many connections were made before it was called. It dials a sentinel connection and reads
// accepted connections until it reaches the sentinel: accepts are served in arrival order, so every
// earlier connection is counted, however late the accept loop was scheduled.
func countConnections(t *testing.T) (string, func() int) {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	remotes := make(chan string, acceptBuffer)

	go func() {
		defer close(remotes)

		for {
			conn, acceptErr := listener.Accept()
			if errors.Is(acceptErr, net.ErrClosed) {
				return
			}

			if acceptErr == nil {
				remotes <- conn.RemoteAddr().String()

				_ = conn.Close()
			}
		}
	}()

	address := listener.Addr().String()

	return address, func() int {
		t.Helper()

		sentinel, dialErr := (&net.Dialer{}).DialContext(t.Context(), "tcp", address)
		require.NoError(t, dialErr)

		defer func() { _ = sentinel.Close() }()

		timeout := time.After(sentinelTimeout)
		earlier := 0

		for {
			select {
			case remote, open := <-remotes:
				require.True(
					t,
					open,
					"the listener closed before the sentinel connection was accepted",
				)

				if remote == sentinel.LocalAddr().String() {
					return earlier
				}

				earlier++
			case <-timeout:
				require.FailNow(t, "the sentinel connection was never accepted")
			}
		}
	}
}

const (
	// acceptBuffer bounds how many connections the accept loop records before it blocks.
	acceptBuffer = 64
	// sentinelTimeout bounds the wait for the accept loop to reach the sentinel connection.
	sentinelTimeout = 10 * time.Second
)

// TestEvaluate_NetworkContextIsNeverLoaded pins that the offline evaluator opens no connection for a
// policy's context entries. The engine has no Kubernetes client, registry client, ConfigMap resolver
// or global context store, so Kyverno disables every network-backed loader; this is why the Kyverno
// and credential-helper advisories in .govulncheck-allow.txt are not exploitable through ksail.
func TestEvaluate_NetworkContextIsNeverLoaded(t *testing.T) {
	t.Parallel()

	address, connectionsMade := countConnections(t)
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{
		policy(t, sprintf(networkContextPolicy, address)),
	}, nil)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(
		t,
		violations,
		networkContextRules,
		"every rule must run, or its context was never reached",
	)
	assert.Zero(t, connectionsMade(), "evaluating a policy must not open a network connection")
}
