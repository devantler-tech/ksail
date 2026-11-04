package gen_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/stretchr/testify/require"
)

// TestGenAndValidateFindsTheHelmReleaseSchema is the negative control for the HelmRelease
// cases in TestGenAndValidate. Those run with IgnoreMissingSchemas, so they would pass
// unexamined if the HelmRelease schema could not be found; this health check lacks the
// required current expression and must be refused under the same options.
func TestGenAndValidateFindsTheHelmReleaseSchema(t *testing.T) {
	t.Parallel()

	manifest := filepath.Join(t.TempDir(), "helmrelease.yaml")
	require.NoError(t, os.WriteFile(manifest, []byte(`apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: control
  namespace: default
spec:
  interval: 1m
  chartRef:
    kind: OCIRepository
    name: control
  healthCheckExprs:
  - apiVersion: bitnami.com/v1alpha1
    kind: SealedSecret
    failed: "true"
`), 0o600))

	err := kubeconform.NewClient().ValidateFile(context.Background(), manifest,
		&kubeconform.ValidationOptions{IgnoreMissingSchemas: true, SkipKinds: []string{}})
	require.ErrorContains(t, err, "current", "a health check without current must be refused")
}
