package image_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/image"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	diagnosticsNginxRef       = "docker.io/library/nginx:latest"
	diagnosticsMissingContent = "ctr: failed to get reader: content digest sha256:missing: not found"
	diagnosticsPullFailure    = "ctr: pull failed: registry unreachable"
)

// TestExportRepairFailureIsReported pins that a failed content repair is visible in the
// returned error: the repair was attempted, for which image, and why it failed. Without
// that, a missing-content export error reads the same whether the repair ran or not.
func TestExportRepairFailureIsReported(t *testing.T) {
	t.Parallel()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupKindNodeListMock(ctx, mockClient)
	setupEmptyImageListMockForExporter(ctx, t, mockClient, kindExporterNodeName)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportCmd := buildKindCtrExportCommand(diagnosticsNginxRef)

	// Bulk export fails on missing content, and the bulk repair's pull fails.
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, diagnosticsMissingContent)
	expectRepairPullFailure(ctx, t, mockClient, diagnosticsNginxRef, diagnosticsPullFailure)

	// The one-by-one fallback hits the same cause and its repair fails the same way.
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, diagnosticsMissingContent)
	expectRepairPullFailure(ctx, t, mockClient, diagnosticsNginxRef, diagnosticsPullFailure)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	err := image.NewExporter(mockClient).Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"nginx:latest"},
		},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "content repair of "+diagnosticsNginxRef+" failed")
	assert.Contains(t, err.Error(), "registry unreachable")
	assert.Contains(t, err.Error(), "individual export errors: "+diagnosticsNginxRef+": ")
	assert.Contains(t, err.Error(), "(content repair failed: ")
}

// TestExportSkippedRepairIsReported pins that a whole-cluster export states that it did
// not attempt the bulk content repair, rather than returning the bare export error.
func TestExportSkippedRepairIsReported(t *testing.T) {
	t.Parallel()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupKindNodeListMock(ctx, mockClient)
	setupExecMockWithStdoutForExporter(
		ctx, t, mockClient, kindExporterNodeName,
		[]string{ctrCommand, "--namespace=k8s.io", "images", "list", "-q"},
		diagnosticsNginxRef+"\n",
	)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportCmd := buildKindCtrExportCommand(diagnosticsNginxRef)
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, diagnosticsMissingContent)

	// The one-by-one fallback still repairs a single image; make that fail too so the
	// whole export fails and the bulk error is returned.
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, diagnosticsMissingContent)
	expectRepairPullFailure(ctx, t, mockClient, diagnosticsNginxRef, diagnosticsPullFailure)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	err := image.NewExporter(mockClient).Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{OutputPath: outputPath},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "content repair skipped")
	assert.NotContains(t, err.Error(), "content repair of")
}

// TestExportRepairedReExportFailureIsReported pins the third outcome: the content repair
// ran and succeeded, yet the re-export still failed. It must read differently from both
// a failed repair and a skipped one.
func TestExportRepairedReExportFailureIsReported(t *testing.T) {
	t.Parallel()

	ctx, mockClient, outputPath := newExporterTestContext(t)
	setupKindNodeListMock(ctx, mockClient)
	setupEmptyImageListMockForExporter(ctx, t, mockClient, kindExporterNodeName)
	setupPlatformDetectMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	exportCmd := buildKindCtrExportCommand(diagnosticsNginxRef)

	// Bulk: missing content, a successful repair, re-resolution, then a failed re-export.
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, diagnosticsMissingContent)
	expectRepairPullSuccess(ctx, t, mockClient, diagnosticsNginxRef)
	setupEmptyImageListMockForExporter(ctx, t, mockClient, kindExporterNodeName)
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, diagnosticsMissingContent)

	// Fallback: the same sequence for the single image.
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, diagnosticsMissingContent)
	expectRepairPullSuccess(ctx, t, mockClient, diagnosticsNginxRef)
	setupKindExecFailWithCmdForExporter(ctx, t, mockClient, exportCmd, diagnosticsMissingContent)
	setupExecMockForExporter(ctx, t, mockClient, kindExporterNodeName)

	err := image.NewExporter(mockClient).Export(
		ctx,
		"my-cluster",
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderDocker,
		image.ExportOptions{
			OutputPath: outputPath,
			Images:     []string{"nginx:latest"},
		},
	)

	require.Error(t, err)
	assert.Contains(
		t,
		err.Error(),
		"content repair of "+diagnosticsNginxRef+" succeeded, but the re-export failed",
	)
	assert.Contains(t, err.Error(), "(content repair succeeded, but the re-export failed: ")
	assert.NotContains(t, err.Error(), "content repair skipped")
	assert.NotContains(t, err.Error(), " failed: ctr: pull failed")
}
