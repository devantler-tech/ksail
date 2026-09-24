package workload_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkloadConfigAllowsLegacyTalosISO keeps workload operations independent of bootstrap pins.
//
//nolint:paralleltest // changes the process working directory
func TestWorkloadConfigAllowsLegacyTalosISO(t *testing.T) {
	t.Chdir(t.TempDir())

	workingDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile("ksail.yaml", []byte(`apiVersion: ksail.io/v1alpha1
kind: Cluster
metadata:
  name: legacy-image
spec:
  cluster:
    distribution: Talos
    provider: Hetzner
    connection:
      context: admin@legacy-image
    talos:
      iso: 123456
  workload:
    sourceDirectory: workloads
`), 0o600))

	for name, load := range map[string]func(*cobra.Command) (*v1alpha1.Cluster, error){
		"reconcile push watch": workload.ExportLoadWorkloadConfig,
		"image export import":  workload.ExportLoadImageConfig,
	} {
		t.Run(name, func(t *testing.T) {
			cmd := &cobra.Command{}
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cfg, err := load(cmd)
			require.NoError(t, err)
			assert.Equal(t, "admin@legacy-image", cfg.Spec.Cluster.Connection.Context)
			assert.Equal(
				t,
				filepath.Join(workingDir, "workloads"),
				cfg.Spec.Workload.SourceDirectory,
			)
		})
	}
}
