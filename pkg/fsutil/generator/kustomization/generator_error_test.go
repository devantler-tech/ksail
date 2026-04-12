package kustomizationgenerator_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	kustomizationgenerator "github.com/devantler-tech/ksail/v6/pkg/fsutil/generator/kustomization"
	yamlgenerator "github.com/devantler-tech/ksail/v6/pkg/fsutil/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

var (
	errKustomizationMarshal         = errors.New("marshal error")
	errKustomizationUnmarshal       = errors.New("unmarshal error")
	errKustomizationUnmarshalString = errors.New("unmarshal string error")
)

// errorMarshaller is a test double that always returns an error from Marshal.
type errorMarshaller struct{}

func (e *errorMarshaller) Marshal(_ *ktypes.Kustomization) (string, error) {
	return "", errKustomizationMarshal
}

func (e *errorMarshaller) Unmarshal(_ []byte, _ **ktypes.Kustomization) error {
	return errKustomizationUnmarshal
}

func (e *errorMarshaller) UnmarshalString(_ string, _ **ktypes.Kustomization) error {
	return errKustomizationUnmarshalString
}

// TestGenerate_MarshalError verifies that a marshalling failure is propagated
// as a wrapped error.
func TestGenerate_MarshalError(t *testing.T) {
	t.Parallel()

	g := &kustomizationgenerator.Generator{
		Marshaller: &errorMarshaller{},
	}

	_, err := g.Generate(&ktypes.Kustomization{}, yamlgenerator.Options{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal kustomization")
}

// TestGenerate_WriteError verifies that a write failure (e.g. read-only
// directory) is propagated as a wrapped error.
func TestGenerate_WriteError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	readOnlyDir := filepath.Join(tmpDir, "readonly")

	err := os.MkdirAll(readOnlyDir, 0o500)
	require.NoError(t, err)

	t.Cleanup(func() {
		//nolint:gosec // restoring test-directory permissions
		_ = os.Chmod(readOnlyDir, 0o700)
	})

	generator := kustomizationgenerator.NewGenerator()

	opts := yamlgenerator.Options{
		Output: filepath.Join(readOnlyDir, "subdir", "kustomization.yaml"),
		Force:  true,
	}

	_, err = generator.Generate(&ktypes.Kustomization{}, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write kustomization")
}
