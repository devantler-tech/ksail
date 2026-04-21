package kubeconform_test

import (
	"context"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBytes_SchemaValidationError(t *testing.T) {
	t.Parallel()

	invalidManifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
data: "this is not valid for strict mode"
`

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		Strict:               true,
		IgnoreMissingSchemas: false,
	}

	err := client.ValidateBytes(context.Background(), "test.yaml", []byte(invalidManifest), opts)
	require.Error(t, err)
	require.ErrorIs(t, err, kubeconform.ErrValidationFailed)
	assert.Contains(t, err.Error(), "validation failed")
}

func TestValidateBytes_NilOptions(t *testing.T) {
	t.Parallel()

	validYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: test-namespace
`

	client := kubeconform.NewClient()

	err := client.ValidateBytes(context.Background(), "test.yaml", []byte(validYAML), nil)
	require.NoError(t, err)
}

func TestValidateBytes_SkipKinds(t *testing.T) {
	t.Parallel()

	//nolint:gosec // G101: This is a test manifest, not a hardcoded credential
	secretYAML := `apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: default
type: Opaque
data:
  key: dmFsdWU=
`

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		SkipKinds:            []string{"Secret"},
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	err := client.ValidateBytes(context.Background(), "secret.yaml", []byte(secretYAML), opts)
	require.NoError(t, err)
}

func TestValidateBytes_MultiDocument(t *testing.T) {
	t.Parallel()

	multiDocYAML := `apiVersion: v1
kind: Namespace
metadata:
  name: test-ns
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: test-ns
data:
  key: value
`

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	err := client.ValidateBytes(context.Background(), "multi.yaml", []byte(multiDocYAML), opts)
	require.NoError(t, err)
}

func TestValidateBytes_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	client := kubeconform.NewClient()

	err := client.ValidateBytes(ctx, "test.yaml", []byte("apiVersion: v1"), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestValidateFile_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	client := kubeconform.NewClient()

	err := client.ValidateFile(ctx, "/some/file.yaml", nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestValidateManifests_CancelledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	client := kubeconform.NewClient()

	err := client.ValidateManifests(ctx, strings.NewReader("apiVersion: v1"), nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestValidateManifests_SkipKinds(t *testing.T) {
	t.Parallel()

	yaml := `apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: default
type: Opaque
data:
  key: dmFsdWU=
`

	client := kubeconform.NewClient()
	opts := &kubeconform.ValidationOptions{
		SkipKinds:            []string{"Secret"},
		Strict:               true,
		IgnoreMissingSchemas: true,
	}

	err := client.ValidateManifests(context.Background(), strings.NewReader(yaml), opts)
	require.NoError(t, err)
}

func TestErrValidationFailed(t *testing.T) {
	t.Parallel()

	require.Error(t, kubeconform.ErrValidationFailed)
	assert.Equal(t, "validation failed", kubeconform.ErrValidationFailed.Error())
}
