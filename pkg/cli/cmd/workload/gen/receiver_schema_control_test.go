package gen_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/stretchr/testify/require"
)

// TestGenAndValidateFindsTheReceiverSchema is the negative control for the receiver cases in
// TestGenAndValidate. Those run with IgnoreMissingSchemas, so they would pass unexamined if
// the Receiver schema could not be found; this Receiver breaks the schema and must be refused
// under the same options.
func TestGenAndValidateFindsTheReceiverSchema(t *testing.T) {
	t.Parallel()

	manifest := filepath.Join(t.TempDir(), "receiver.yaml")
	require.NoError(t, os.WriteFile(manifest, []byte(`apiVersion: notification.toolkit.fluxcd.io/v1
kind: Receiver
metadata:
  name: control
  namespace: flux-system
spec:
  type: generic-oidc
  oidcProviders:
  - issuerURL: https://token.actions.githubusercontent.com
    validations:
    - expression: claims.repository_owner == 'my-org'
  resources:
  - kind: GitRepository
    name: webapp
`), 0o600))

	err := kubeconform.NewClient().ValidateFile(context.Background(), manifest,
		&kubeconform.ValidationOptions{IgnoreMissingSchemas: true, SkipKinds: []string{}})
	require.ErrorContains(
		t,
		err,
		"message",
		"a validation without its required message must be refused",
	)
}
