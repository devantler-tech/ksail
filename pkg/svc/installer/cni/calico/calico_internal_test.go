package calicoinstaller_test

import (
	"errors"
	"fmt"
	"testing"

	calicoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/calico"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var (
	errCalicoNoMatchesInstallation = errors.New(`no matches for kind "Installation"`)
	errCalicoRequestedResource     = errors.New("could not find the requested resource")
)

func TestTalosCalicoValues(t *testing.T) {
	t.Parallel()

	values := calicoinstaller.TalosCalicoValuesForTest()

	require.NotEmpty(t, values)
	assert.Contains(t, values, "installation.kubeletVolumePluginPath")
	assert.Equal(t, `"None"`, values["installation.kubeletVolumePluginPath"])
	assert.Contains(t, values, "installation.calicoNetwork.linuxDataplane")
	assert.Equal(t, `"Nftables"`, values["installation.calicoNetwork.linuxDataplane"])
	assert.Contains(t, values, "installation.calicoNetwork.bgp")
	assert.Equal(t, `"Disabled"`, values["installation.calicoNetwork.bgp"])
}

func TestDefaultCalicoValues(t *testing.T) {
	t.Parallel()

	values := calicoinstaller.DefaultCalicoValuesForTest()
	assert.Empty(t, values, "default values should be empty map")
}

func TestCalicoCRDNames(t *testing.T) {
	t.Parallel()

	names := calicoinstaller.CalicoCRDNamesForTest()

	require.NotEmpty(t, names)
	assert.Contains(t, names, "installations.operator.tigera.io")
	assert.Contains(t, names, "tigerastatuses.operator.tigera.io")
}

func TestCalicoNamespaces(t *testing.T) {
	t.Parallel()

	namespaces := calicoinstaller.CalicoNamespacesForTest()

	require.Len(t, namespaces, 2)
	assert.Contains(t, namespaces, "tigera-operator")
	assert.Contains(t, namespaces, "calico-system")
}

func TestIsAPIDiscoveryError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "generic error", err: assert.AnError, want: false},
		{
			name: "no matches for kind",
			err:  fmt.Errorf("%w", errCalicoNoMatchesInstallation),
			want: true,
		},
		{
			name: "could not find resource",
			err:  fmt.Errorf("%w", errCalicoRequestedResource),
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := calicoinstaller.IsAPIDiscoveryErrorForTest(tc.err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIsCRDEstablished(t *testing.T) {
	t.Parallel()

	t.Run("established CRD returns true", func(t *testing.T) {
		t.Parallel()

		crd := &apiextensionsv1.CustomResourceDefinition{
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
					{
						Type:   apiextensionsv1.Established,
						Status: apiextensionsv1.ConditionTrue,
					},
				},
			},
		}
		assert.True(t, calicoinstaller.IsCRDEstablishedForTest(crd))
	})

	t.Run("not established CRD returns false", func(t *testing.T) {
		t.Parallel()

		crd := &apiextensionsv1.CustomResourceDefinition{
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
					{
						Type:   apiextensionsv1.Established,
						Status: apiextensionsv1.ConditionFalse,
					},
				},
			},
		}
		assert.False(t, calicoinstaller.IsCRDEstablishedForTest(crd))
	})

	t.Run("no conditions returns false", func(t *testing.T) {
		t.Parallel()

		crd := &apiextensionsv1.CustomResourceDefinition{}
		assert.False(t, calicoinstaller.IsCRDEstablishedForTest(crd))
	})

	t.Run("multiple conditions with established", func(t *testing.T) {
		t.Parallel()

		crd := &apiextensionsv1.CustomResourceDefinition{
			Status: apiextensionsv1.CustomResourceDefinitionStatus{
				Conditions: []apiextensionsv1.CustomResourceDefinitionCondition{
					{
						Type:   apiextensionsv1.NamesAccepted,
						Status: apiextensionsv1.ConditionTrue,
					},
					{
						Type:   apiextensionsv1.Established,
						Status: apiextensionsv1.ConditionTrue,
					},
				},
			},
		}
		assert.True(t, calicoinstaller.IsCRDEstablishedForTest(crd))
	})
}
