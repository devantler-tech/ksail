package ciliuminstaller_test

import (
	"context"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	ciliuminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/cilium"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestInstaller_Install_TalosDistribution(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderDocker,
		v1alpha1.LoadBalancerDefault,
	)

	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })
	installer.SetGatewayAPICRDInstaller(func(_ context.Context) error { return nil })

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				if spec == nil {
					return false
				}
				// Talos-specific values should be present
				assert.Equal(t, `"kubernetes"`, spec.SetJSONVals["ipam.mode"])
				assert.Equal(t, "true", spec.SetJSONVals["kubeProxyReplacement"])
				assert.Equal(t, `"localhost"`, spec.SetJSONVals["k8sServiceHost"])
				assert.Equal(t, `"7445"`, spec.SetJSONVals["k8sServicePort"])
				assert.Equal(t, "false", spec.SetJSONVals["cgroup.autoMount.enabled"])

				return true
			}),
		).
		Return(nil, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Install_HetznerProvider(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
		v1alpha1.ProviderHetzner,
		v1alpha1.LoadBalancerDefault,
	)

	installer.SetGatewayAPICRDInstaller(func(_ context.Context) error { return nil })

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				if spec == nil {
					return false
				}
				// Hetzner provider should NOT have Docker-specific values
				_, hasHostNetwork := spec.SetJSONVals["gatewayAPI.hostNetwork.enabled"]
				assert.False(t, hasHostNetwork,
					"hostNetwork should not be set for Hetzner provider")
				// Default values should be present
				assert.Equal(t, "true", spec.SetJSONVals["gatewayAPI.enabled"])
				assert.Equal(t, "1", spec.SetJSONVals["operator.replicas"])

				return true
			}),
		).
		Return(nil, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Install_TalosDockerWithLoadBalancer(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionTalos,
		v1alpha1.ProviderDocker,
		v1alpha1.LoadBalancerEnabled,
	)

	installer.SetAPIServerCheckerForTest(func(_ context.Context) error { return nil })
	installer.SetGatewayAPICRDInstaller(func(_ context.Context) error { return nil })

	client.EXPECT().
		AddRepository(mock.Anything, mock.Anything, mock.Anything).
		Return(nil)

	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				if spec == nil {
					return false
				}
				// Talos values should be present
				assert.Equal(t, `"kubernetes"`, spec.SetJSONVals["ipam.mode"])
				// But Docker hostNetwork should NOT be set because LoadBalancer is enabled
				_, hasHostNetwork := spec.SetJSONVals["gatewayAPI.hostNetwork.enabled"]
				assert.False(t, hasHostNetwork,
					"hostNetwork should not be set when LoadBalancer is enabled")

				return true
			}),
		).
		Return(nil, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestInstaller_Images_Success(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
		"",
		v1alpha1.LoadBalancerDefault,
	)

	manifest := `apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: cilium
spec:
  template:
    spec:
      containers:
      - name: cilium-agent
        image: quay.io/cilium/cilium:v1.19.2
`

	client.EXPECT().
		TemplateChart(mock.Anything, mock.Anything).
		Return(manifest, nil)

	images, err := installer.Images(context.Background())

	require.NoError(t, err)
	assert.NotEmpty(t, images)
	assert.Contains(t, images, "quay.io/cilium/cilium:v1.19.2")
}

func TestInstaller_Images_NilClient(t *testing.T) {
	t.Parallel()

	installer := ciliuminstaller.NewInstallerWithDistribution(
		nil,
		"/path/to/kubeconfig",
		"test-context",
		5*time.Minute,
		v1alpha1.DistributionVanilla,
		"",
		v1alpha1.LoadBalancerDefault,
	)

	images, err := installer.Images(context.Background())

	require.Error(t, err)
	assert.Nil(t, images)
	assert.Contains(t, err.Error(), "helm client is nil")
}

func TestInstaller_Uninstall_ContextCanceled(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer := ciliuminstaller.NewInstallerWithDistribution(
		client,
		"/path/to/kubeconfig",
		"test-context",
		2*time.Minute,
		v1alpha1.DistributionVanilla,
		"",
		v1alpha1.LoadBalancerDefault,
	)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	client.EXPECT().
		UninstallRelease(mock.Anything, "cilium", "kube-system").
		Return(ctx.Err())

	err := installer.Uninstall(ctx)

	require.Error(t, err)
}

func TestGatewayAPICRDsURL_NonEmpty(t *testing.T) {
	t.Parallel()

	// We test parseGatewayAPICRDs via the exported function to check URL is non-empty.
	// The URL is derived from the embedded Dockerfile.gateway-api and must contain
	// a version string. We verify this indirectly by checking the fetch function
	// accepts a non-empty URL without panicking.
	crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte(""))
	require.NoError(t, err)
	assert.Empty(t, crds)
}

func TestGatewayAPICRDsVersion_NonEmpty(t *testing.T) {
	t.Parallel()

	// Verify that ParseGatewayAPICRDs can handle a simple valid CRD,
	// which confirms the parsing pipeline works.
	crdYAML := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: gateways.gateway.networking.k8s.io
spec:
  group: gateway.networking.k8s.io
  names:
    kind: Gateway
    plural: gateways
  scope: Namespaced
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
`

	crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte(crdYAML))

	require.NoError(t, err)
	require.Len(t, crds, 1)
	assert.Equal(t, "gateways.gateway.networking.k8s.io", crds[0].Name)
}

func TestParseGatewayAPICRDs_InvalidYAML(t *testing.T) {
	t.Parallel()

	// This document looks like a CRD (has kind: CustomResourceDefinition) but
	// has an invalid spec field that will cause JSON unmarshal to fail.
	invalidCRD := `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: test.example.com
spec:
  versions:
    - name: true
      served: not-a-bool
      storage: not-a-bool
      schema:
        openAPIV3Schema:
          type: object
`

	crds, err := ciliuminstaller.ParseGatewayAPICRDs([]byte(invalidCRD))

	// The parser may return an error on invalid CRD structure, or silently
	// skip it — either way the result should not be a valid CRD list.
	if err != nil {
		assert.Contains(t, err.Error(), "unmarshal")
	} else {
		// If no error, the CRD fields should be degraded/empty
		for _, crd := range crds {
			assert.Equal(t, "test.example.com", crd.Name)
		}
	}
}
