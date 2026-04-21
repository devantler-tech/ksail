package ciliuminstaller_test

import (
	"testing"

	ciliuminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/cilium"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGatewayAPICRDs_ValidCRD(t *testing.T) {
	t.Parallel()

	validCRD := `---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: test-valid.example.com
spec:
  group: example.com
  names:
    kind: TestValid
    plural: testvalids
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`
	crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte(validCRD))
	require.NoError(t, err)
	assert.Len(t, crds, 1)
	assert.Equal(t, "test-valid.example.com", crds[0].Name)
}

func TestParseGatewayAPICRDs_YAMLSeparatorsOnly(t *testing.T) {
	t.Parallel()

	crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte("---\n---\n---\n"))
	require.NoError(t, err)
	assert.Empty(t, crds)
}

func TestParseGatewayAPICRDs_NonCRDKindsFiltered(t *testing.T) {
	t.Parallel()

	bundle := `---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: gateway-sa
---
apiVersion: v1
kind: Service
metadata:
  name: gateway-svc
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gateway-deploy
`
	crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte(bundle))
	require.NoError(t, err)
	assert.Empty(t, crds)
}
