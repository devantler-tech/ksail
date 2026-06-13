package cluster_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const mirrorRegistryHelp = "Configure mirror registries with format 'host=upstream' " +
	"(e.g., docker.io=https://registry-1.docker.io)."

func TestValidateOutputFormat_ValidText(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().String("output", string(cluster.ExportOutputFormatText), "output format")
	assert.NoError(t, cluster.ExportValidateOutputFormat(cmd))
}

func TestValidateOutputFormat_ValidJSON(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().String("output", string(cluster.ExportOutputFormatJSON), "output format")
	assert.NoError(t, cluster.ExportValidateOutputFormat(cmd))
}

func TestValidateOutputFormat_Invalid(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	cmd.Flags().String("output", "xml", "output format")
	err := cluster.ExportValidateOutputFormat(cmd)
	require.Error(t, err)
	assert.ErrorIs(t, err, cluster.ErrUnsupportedOutputFormat)
}

func TestValidateOutputFormat_NilCmd(t *testing.T) {
	t.Parallel()

	// nil cmd should default to "text" and pass validation
	assert.NoError(t, cluster.ExportValidateOutputFormat(nil))
}

func TestValidateOutputFormat_NoOutputFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{}
	// No --output flag registered, should default to text
	assert.NoError(t, cluster.ExportValidateOutputFormat(cmd))
}

func TestOutputFormatConstants(t *testing.T) {
	t.Parallel()
	assert.NotEmpty(t, cluster.ExportOutputFormatJSON)
	assert.NotEmpty(t, cluster.ExportOutputFormatText)
	assert.NotEqual(t, cluster.ExportOutputFormatJSON, cluster.ExportOutputFormatText)
}
