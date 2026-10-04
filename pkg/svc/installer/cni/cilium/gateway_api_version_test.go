package ciliuminstaller_test

import (
	"testing"

	ciliuminstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/cilium"
	"github.com/stretchr/testify/assert"
	gatewayapiconsts "sigs.k8s.io/gateway-api/pkg/consts"
)

// The Gateway API CRDs KSail installs are the release of the sigs.k8s.io/gateway-api module this
// binary links. That module is a direct requirement in go.mod, so Dependabot proposes its
// releases. A version kept anywhere else has nothing publishing updates for it: the previous pin
// named an image that stopped at v1.0.0 and never moved again (ksail#7472).
func TestGatewayAPICRDsVersionIsTheLinkedModuleRelease(t *testing.T) {
	t.Parallel()

	assert.Equal(
		t,
		gatewayapiconsts.BundleVersion,
		"v"+ciliuminstaller.GatewayAPICRDsVersionForTest(),
		"the installed Gateway API CRDs must come from the linked sigs.k8s.io/gateway-api release",
	)
	assert.Equal(
		t,
		"https://github.com/kubernetes-sigs/gateway-api/releases/download/"+
			gatewayapiconsts.BundleVersion+"/experimental-install.yaml",
		ciliuminstaller.GatewayAPICRDsURLForTest(),
	)
}
