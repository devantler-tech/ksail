package kindprovisioner

import (
	"errors"
	"strings"

	kindexec "sigs.k8s.io/kind/pkg/exec"
)

const maxKindBootstrapDiagnosticBytes = 64 << 10

// Kind captures bootstrap output in RunError but its Error method omits it.
// Emit only fixed diagnostic labels: kubeadm output can contain bootstrap
// credentials, private endpoints and configuration that must not reach CI logs.
func kindBootstrapDiagnostic(err error) string {
	var commandError *kindexec.RunError
	if !errors.As(err, &commandError) {
		return ""
	}

	output := commandError.Output
	if len(output) > maxKindBootstrapDiagnosticBytes {
		output = output[len(output)-maxKindBootstrapDiagnosticBytes:]
	}

	text := strings.ToLower(string(output))

	var labels []string

	for _, signal := range []struct{ needle, label string }{
		{"x509:", "tls-verification"},
		{"no such host", "dns-resolution"},
		{"connection refused", "connection-refused"},
		{"i/o timeout", "network-timeout"},
		{"tls handshake timeout", "tls-handshake-timeout"},
		{"context deadline exceeded", "deadline"},
		{"could not find a jws signature", "bootstrap-signature"},
		{"kubelet is not healthy", "kubelet-health"},
		{"port 10250 is in use", "kubelet-port-conflict"},
		{"cri v1 runtime api is not implemented", "container-runtime"},
		{"unauthorized", "authentication"},
		{"timed out waiting for the condition", "condition-timeout"},
		{"error execution phase kubelet-start:", "kubelet-start-stage"},
		{"error execution phase preflight:", "preflight-stage"},
		{"failed while waiting for tls bootstrap", "kubelet-tls-bootstrap"},
	} {
		if strings.Contains(text, signal.needle) {
			labels = append(labels, signal.label)
		}
	}

	if len(labels) == 0 {
		labels = append(labels, "unclassified")
	}

	return " (bootstrap diagnostics: " + strings.Join(labels, ", ") + ")"
}
