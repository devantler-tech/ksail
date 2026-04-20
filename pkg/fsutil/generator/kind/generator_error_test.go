package kindgenerator_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	kindgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/kind"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

var (
	errKindMarshal         = errors.New("marshal error")
	errKindUnmarshal       = errors.New("unmarshal error")
	errKindUnmarshalString = errors.New("unmarshal string error")
)

// errorKindMarshaller is a test double that always returns a marshal error.
type errorKindMarshaller struct{}

func (e *errorKindMarshaller) Marshal(_ *v1alpha4.Cluster) (string, error) {
	return "", errKindMarshal
}

func (e *errorKindMarshaller) Unmarshal(_ []byte, _ **v1alpha4.Cluster) error {
	return errKindUnmarshal
}

func (e *errorKindMarshaller) UnmarshalString(_ string, _ **v1alpha4.Cluster) error {
	return errKindUnmarshalString
}

// TestGenerate_MarshalError verifies that a marshalling error is propagated.
func TestGenerate_MarshalError(t *testing.T) {
	t.Parallel()

	g := &kindgenerator.Generator{
		Marshaller: &errorKindMarshaller{},
	}

	_, err := g.Generate(&v1alpha4.Cluster{}, yamlgenerator.Options{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal kind config")
}

// TestGenerate_WriteError verifies that a file write error is propagated.
func TestGenerate_WriteError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	blockingPath := filepath.Join(tmpDir, "blocking-file")
	require.NoError(t, os.WriteFile(blockingPath, []byte("blocker"), 0o600))

	generator := kindgenerator.NewGenerator()

	opts := yamlgenerator.Options{
		Output: filepath.Join(blockingPath, "subdir", "kind.yaml"),
		Force:  true,
	}

	_, err := generator.Generate(&v1alpha4.Cluster{}, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write kind config")
}
