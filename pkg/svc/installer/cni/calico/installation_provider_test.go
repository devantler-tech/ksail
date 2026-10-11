package calicoinstaller_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	calicoinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/cni/calico"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v3"
)

// TestCalicoInstallationProviderMatchesBackend catches an empty provider being
// applied over the operator's detected Kind value, without treating native
// kubeadm or another distribution as Kind. Render the complete published chart:
// its Deployment also requires the provider to remain a string.
func TestCalicoInstallationProviderMatchesBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		provider     v1alpha1.Provider
		want         string
	}{
		{"kind Docker", v1alpha1.DistributionVanilla, v1alpha1.ProviderDocker, "Kind"},
		{"kind Kubernetes", v1alpha1.DistributionVanilla, v1alpha1.ProviderKubernetes, "Kind"},
		{"native kubeadm", v1alpha1.DistributionVanilla, v1alpha1.ProviderHetzner, ""},
		{"unknown backend", v1alpha1.DistributionVanilla, "", ""},
		{"k3s", v1alpha1.DistributionK3s, v1alpha1.ProviderDocker, ""},
		{"vcluster", v1alpha1.DistributionVCluster, v1alpha1.ProviderDocker, ""},
		{"talos nested", v1alpha1.DistributionTalos, v1alpha1.ProviderKubernetes, ""},
		{"talos Docker", v1alpha1.DistributionTalos, v1alpha1.ProviderDocker, ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			inst := calicoinstaller.NewInstaller(
				nil,
				"",
				"",
				time.Second,
				testCase.distribution,
				true,
				calicoinstaller.WithKubernetesProviderNetwork(testCase.provider,
					v1alpha1.OptionsKubernetes{PodCIDR: "10.80.0.0/16"}),
			)
			spec := calicoinstaller.ChartSpecForTest(inst)
			chart, err := filepath.Abs("testdata/tigera-operator-v3.33.0.tgz")
			require.NoError(t, err)

			spec.ChartName, spec.RepoURL = chart, ""
			client, err := helm.NewTemplateOnlyClient()
			require.NoError(t, err)
			manifest, err := client.TemplateChart(context.Background(), spec)
			require.NoError(t, err)

			want := expectedInstallationSpec(
				testCase.distribution,
				testCase.provider,
				testCase.want,
			)
			assert.Equal(t, want, installationSpec(t, manifest))
		})
	}
}

func expectedInstallationSpec(
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
	kubernetesProvider string,
) map[string]any {
	want := map[string]any{
		"controlPlaneNodeSelector": map[string]any{}, "controlPlaneReplicas": 2,
		"controlPlaneTolerations": []any{}, "imagePullSecrets": []any{},
		"kubeletVolumePluginPath": "None", "kubernetesProvider": kubernetesProvider,
		"nonPrivileged": "Disabled",
	}

	if distribution == v1alpha1.DistributionTalos {
		cidr := "10.244.0.0/16"
		if provider == v1alpha1.ProviderKubernetes {
			cidr = "10.80.0.0/16"
		}

		want["calicoNetwork"] = map[string]any{
			"linuxDataplane": "Nftables", "bgp": "Disabled",
			"ipPools": []any{map[string]any{
				"name": "default-ipv4-ippool", "blockSize": 26, "cidr": cidr,
				"encapsulation": "VXLAN", "natOutgoing": "Enabled", "nodeSelector": "all()",
			}},
		}
	}

	return want
}

func installationSpec(t *testing.T, manifest string) map[string]any {
	t.Helper()

	decoder := yaml.NewDecoder(strings.NewReader(manifest))

	var found []map[string]any

	for {
		var object struct {
			Kind string         `yaml:"kind"`
			Spec map[string]any `yaml:"spec"`
		}

		err := decoder.Decode(&object)
		if errors.Is(err, io.EOF) {
			break
		}

		require.NoError(t, err)

		if object.Kind == "Installation" {
			found = append(found, object.Spec)
		}
	}

	require.Len(t, found, 1)

	return found[0]
}
