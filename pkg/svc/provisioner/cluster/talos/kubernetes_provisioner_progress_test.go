package talosprovisioner_test

import (
	"bytes"
	"path/filepath"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKubernetesProvisioner_ProgressFollowsInnerLogWriter guards the stdout
// contract of `cluster update --output json` (ksail#7200): a recreation can build
// a Kubernetes-hosted Talos cluster, and its progress must reach the writer the
// factory configured on the inner provisioner (stderr), never os.Stdout.
func TestKubernetesProvisioner_ProgressFollowsInnerLogWriter(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer

	inner := talosprovisioner.NewProvisioner(nil, nil).WithLogWriter(&logs)

	provisioner, err := talosprovisioner.NewKubernetesProvisioner(
		talosprovisioner.KubernetesProvisionerConfig{
			InnerProvisioner: inner,
			ClusterName:      "progress-writer",
			KubeconfigPath:   filepath.Join(t.TempDir(), "kubeconfig"),
		},
	)
	require.NoError(t, err)

	_, err = provisioner.ProgressWriterForTest().Write([]byte("progress line\n"))
	require.NoError(t, err)

	assert.Equal(t, "progress line\n", logs.String())
}
