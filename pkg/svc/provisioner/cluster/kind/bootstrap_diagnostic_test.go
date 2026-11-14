package kindprovisioner_test

import (
	"fmt"
	"strings"
	"testing"

	cmdrunner "github.com/devantler-tech/ksail/v7/pkg/runner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	kinderrors "sigs.k8s.io/kind/pkg/errors"
	kindexec "sigs.k8s.io/kind/pkg/exec"
)

func TestCreateReportsSafeBootstrapDiagnostic(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name   string
		output string
		want   string
	}{
		{name: "dns", output: "lookup private.example: no such host", want: "dns-resolution"},
		{name: "tls", output: "x509: certificate is valid for private.example", want: "tls-verification"},
		{name: "timeout", output: "context deadline exceeded", want: "deadline"},
		{name: "refused", output: "dial tcp: connection refused", want: "connection-refused"},
		{name: "signature", output: "could not find a JWS signature", want: "bootstrap-signature"},
		{name: "unknown", output: "opaque upstream failure", want: "unclassified"},
		{
			name:   "bounded tail",
			output: strings.Repeat("x", 100_000) + "\ncontext deadline exceeded",
			want:   "deadline",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			provisioner, _, runner := newProvisionerForTest(t)
			upstream := &kindexec.RunError{
				Command: []string{"docker", "exec", "worker", "kubeadm", "join"},
				Output:  []byte("sensitive-bootstrap-material\n" + testCase.output),
				Inner:   errCreateClusterFailed,
			}
			runner.On("Run").Return(
				cmdrunner.CommandResult{},
				fmt.Errorf("wrapped: %w", kinderrors.Wrap(upstream, "native Kind wrapper")),
			)

			err := provisioner.Create(t.Context(), "diagnostic-test")

			require.ErrorContains(t, err, "bootstrap diagnostics: "+testCase.want)
			assert.NotContains(t, err.Error(), "sensitive-bootstrap-material")
			assert.NotContains(t, err.Error(), "private.example")
			assert.Less(t, len(err.Error()), 1_000)

			var retained *kindexec.RunError

			require.ErrorAs(t, err, &retained, "retain the original typed failure")
			assert.Same(t, upstream, retained)
		})
	}
}
