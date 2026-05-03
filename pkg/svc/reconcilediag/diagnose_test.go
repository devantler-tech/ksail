package reconcilediag_test

import (
	"bytes"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/reconcilediag"
	"github.com/stretchr/testify/assert"
)

func TestDiagnose_WithEngineNone(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	reconcilediag.Diagnose(t.Context(), &buf, "/any/path", v1alpha1.GitOpsEngineNone)
	assert.Empty(t, buf.String())
}

func TestDiagnose_WithUnknownEngine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	reconcilediag.Diagnose(t.Context(), &buf, "/any/path", v1alpha1.GitOpsEngine("unsupported"))
	assert.Empty(t, buf.String())
}

func TestDiagnose_WithInvalidKubeconfigPath(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	reconcilediag.Diagnose(t.Context(), &buf, t.TempDir()+"/kubeconfig.yaml", v1alpha1.GitOpsEngineFlux)
	assert.Empty(t, buf.String())
}
