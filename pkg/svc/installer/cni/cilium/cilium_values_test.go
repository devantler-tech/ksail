package ciliuminstaller_test

import (
	"strings"
	"testing"

	ciliuminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/cilium"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGatewayAPICRDsVersion_NonEmptyAndSemver(t *testing.T) {
	t.Parallel()

	version := ciliuminstaller.GatewayAPICRDsVersionForTest()

	require.NotEmpty(t, version, "gateway API CRDs version should not be empty")
	// Version should look like a semver (e.g., "1.2.0")
	parts := strings.Split(version, ".")
	assert.GreaterOrEqual(t, len(parts), 2, "version should have at least major.minor")
}

func TestGatewayAPICRDsURL_ContainsVersion(t *testing.T) {
	t.Parallel()

	url := ciliuminstaller.GatewayAPICRDsURLForTest()
	version := ciliuminstaller.GatewayAPICRDsVersionForTest()

	require.NotEmpty(t, url, "gateway API CRDs URL should not be empty")
	assert.Contains(t, url, version, "URL should contain the version string")
	assert.Contains(t, url, "experimental-install.yaml", "URL should reference experimental bundle")
	assert.True(t, strings.HasPrefix(url, "https://"), "URL should use HTTPS")
}

func TestDefaultCiliumValues(t *testing.T) {
	t.Parallel()

	values := ciliuminstaller.DefaultCiliumValuesForTest()

	require.NotEmpty(t, values, "default cilium values should not be empty")
	assert.Equal(t, "1", values["operator.replicas"])
	assert.Equal(t, "true", values["gatewayAPI.enabled"])
}

func TestTalosCiliumValues(t *testing.T) {
	t.Parallel()

	values := ciliuminstaller.TalosCiliumValuesForTest()

	require.NotEmpty(t, values, "talos cilium values should not be empty")
	assert.Equal(t, `"kubernetes"`, values["ipam.mode"])
	assert.Equal(t, "true", values["kubeProxyReplacement"])
	assert.Equal(t, `"localhost"`, values["k8sServiceHost"])
	assert.Equal(t, `"7445"`, values["k8sServicePort"])
	assert.Equal(t, "false", values["cgroup.autoMount.enabled"])
	assert.Equal(t, `"/sys/fs/cgroup"`, values["cgroup.hostRoot"])
	assert.Contains(t, values, "securityContext.capabilities.ciliumAgent")
	assert.Contains(t, values, "securityContext.capabilities.cleanCiliumState")
	// Talos-specific: SYS_MODULE must NOT be in ciliumAgent caps
	assert.NotContains(t, values["securityContext.capabilities.ciliumAgent"], "SYS_MODULE")
}

func TestDockerCiliumValues(t *testing.T) {
	t.Parallel()

	values := ciliuminstaller.DockerCiliumValuesForTest()

	require.NotEmpty(t, values, "docker cilium values should not be empty")
	assert.Equal(t, "true", values["gatewayAPI.hostNetwork.enabled"])
	assert.Equal(t, "true", values["envoy.securityContext.capabilities.keepCapNetBindService"])
	assert.Contains(t, values["envoy.securityContext.capabilities.envoy"], "NET_BIND_SERVICE")
	assert.Contains(t, values["envoy.securityContext.capabilities.envoy"], "NET_ADMIN")
}
