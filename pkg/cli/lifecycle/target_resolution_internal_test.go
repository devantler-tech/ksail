package lifecycle

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// unsetRegionEnvVar names an environment variable no test sets, so region resolution reaches the
// eks.yaml branch without mutating the process environment.
const unsetRegionEnvVar = "KSAIL_LIFECYCLE_TEST_UNSET_REGION"

func eksCluster() *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Name = "meta-name"
	cfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS

	return cfg
}

func eksDistribution(
	name, configPath string,
	nameFromConfig bool,
) *clusterprovisioner.DistributionConfig {
	return &clusterprovisioner.DistributionConfig{
		EKS: &clusterprovisioner.EKSConfig{
			Name:           name,
			NameFromConfig: nameFromConfig,
			ConfigPath:     configPath,
		},
	}
}

type eksConfigSourceCase struct {
	name       string
	cfg        *v1alpha1.Cluster
	distCfg    *clusterprovisioner.DistributionConfig
	configName string
	want       bool
}

type configClusterNameCase struct {
	name    string
	cfg     *v1alpha1.Cluster
	distCfg *clusterprovisioner.DistributionConfig
	want    string
}

// TestResolveAWSRegionReportsConfigSource pins that the region is reported as config-bound only
// when the file that named the cluster also declared a region.
func TestResolveAWSRegionReportsConfigSource(t *testing.T) {
	t.Parallel()

	opts := v1alpha1.OptionsAWS{RegionEnvVar: unsetRegionEnvVar}

	t.Run("named file with region is config-bound", func(t *testing.T) {
		t.Parallel()

		distCfg := eksDistribution("eks-name", "eks.yaml", true)
		distCfg.EKS.Region = "eu-west-1"

		region, fromConfig := resolveAWSRegion(opts, distCfg)
		assert.Equal(t, "eu-west-1", region)
		assert.True(t, fromConfig)
	})

	t.Run("named file without region is not config-bound", func(t *testing.T) {
		t.Parallel()

		region, fromConfig := resolveAWSRegion(opts, eksDistribution("eks-name", "eks.yaml", true))
		assert.Empty(t, region)
		assert.False(t, fromConfig)
	})
}

// TestResolveClusterIdentityKubeconfigPrecedence pins that a kubeconfig path resolved from a flag
// survives a config that also sets one, and that an empty one fills from the config.
func TestResolveClusterIdentityKubeconfigPrecedence(t *testing.T) {
	t.Parallel()

	cfg := &v1alpha1.Cluster{}
	cfg.Name = "meta-name"
	cfg.Spec.Cluster.Connection.Kubeconfig = "from-config"

	t.Run("resolved path survives config path", func(t *testing.T) {
		t.Parallel()

		resolved := &ResolvedClusterInfo{KubeconfigPath: "from-flag"}
		resolveClusterIdentityFromConfig(resolved, cfg, nil)
		assert.Equal(t, "from-flag", resolved.KubeconfigPath)
	})

	t.Run("empty path fills from config", func(t *testing.T) {
		t.Parallel()

		resolved := &ResolvedClusterInfo{}
		resolveClusterIdentityFromConfig(resolved, cfg, nil)
		assert.Equal(t, "from-config", resolved.KubeconfigPath)
	})

	t.Run("empty path stays empty without config path", func(t *testing.T) {
		t.Parallel()

		resolved := &ResolvedClusterInfo{}
		resolveClusterIdentityFromConfig(resolved, &v1alpha1.Cluster{}, nil)
		assert.Empty(t, resolved.KubeconfigPath)
	})
}

// TestHasLoadedEKSConfigSource pins every operand of the guard that lets the actual eks.yaml name
// win over metadata.name.
func TestHasLoadedEKSConfigSource(t *testing.T) {
	t.Parallel()

	tests := []eksConfigSourceCase{
		{
			name:       "loaded named eks file",
			cfg:        eksCluster(),
			distCfg:    eksDistribution("eks-name", "eks.yaml", true),
			configName: "eks-name",
			want:       true,
		},
		{
			name:       "non-eks distribution",
			cfg:        &v1alpha1.Cluster{},
			distCfg:    eksDistribution("eks-name", "eks.yaml", true),
			configName: "eks-name",
		},
		{
			name:       "nil distribution config",
			cfg:        eksCluster(),
			configName: "eks-name",
		},
		{
			name:       "nil eks config",
			cfg:        eksCluster(),
			distCfg:    &clusterprovisioner.DistributionConfig{},
			configName: "eks-name",
		},
		{
			name:       "synthesized name",
			cfg:        eksCluster(),
			distCfg:    eksDistribution("eks-name", "eks.yaml", false),
			configName: "eks-name",
		},
		{
			name:       "no config path",
			cfg:        eksCluster(),
			distCfg:    eksDistribution("eks-name", " ", true),
			configName: "eks-name",
		},
		{
			name:       "blank config name",
			cfg:        eksCluster(),
			distCfg:    eksDistribution("eks-name", "eks.yaml", true),
			configName: " ",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hasLoadedEKSConfigSource(testCase.cfg, testCase.distCfg, testCase.configName)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestResolveClusterNameFromConfigPrecedence pins the fallbacks used when no flag names the
// target: the eks.yaml name, then a valid metadata.name, then the distribution config name.
func TestResolveClusterNameFromConfigPrecedence(t *testing.T) {
	t.Parallel()

	kindDistribution := &clusterprovisioner.DistributionConfig{
		Kind: &v1alpha4.Cluster{Name: "kind-name"},
	}

	tests := []struct {
		name            string
		currentName     string
		eksConfigSource bool
		cfgName         string
		want            string
	}{
		{
			name:            "current name wins",
			currentName:     "flag-name",
			eksConfigSource: true,
			cfgName:         "meta-name",
			want:            "flag-name",
		},
		{
			name:            "eks file name wins over metadata",
			eksConfigSource: true,
			cfgName:         "meta-name",
			want:            "eks-name",
		},
		{
			name:    "valid metadata name wins over distribution",
			cfgName: "meta-name",
			want:    "meta-name",
		},
		{
			name:    "invalid metadata name falls back to distribution",
			cfgName: "Invalid_Name",
			want:    "kind-name",
		},
		{name: "empty metadata name falls back to distribution", want: "kind-name"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cfg := &v1alpha1.Cluster{}
			cfg.Name = testCase.cfgName

			got := resolveClusterNameFromConfig(
				testCase.currentName,
				"eks-name",
				testCase.eksConfigSource,
				cfg,
				kindDistribution,
			)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// TestResolveConfigClusterName pins which name the local config describes: for EKS only the name
// the actual eks.yaml declared, otherwise the distribution name, then a valid metadata.name.
func TestResolveConfigClusterName(t *testing.T) {
	t.Parallel()

	tests := []configClusterNameCase{
		{
			name:    "eks file name is trimmed",
			cfg:     eksCluster(),
			distCfg: eksDistribution(" eks-name ", "eks.yaml", true),
			want:    "eks-name",
		},
		{
			name:    "eks without config path describes nothing",
			cfg:     eksCluster(),
			distCfg: eksDistribution("eks-default", " ", true),
			want:    "",
		},
		{
			name:    "eks with synthesized name describes nothing",
			cfg:     eksCluster(),
			distCfg: eksDistribution("eks-default", "eks.yaml", false),
			want:    "",
		},
		{
			name: "eks without eks config falls back to distribution name",
			cfg:  eksCluster(),
			distCfg: &clusterprovisioner.DistributionConfig{
				Kind: &v1alpha4.Cluster{Name: "kind-name"},
			},
			want: "kind-name",
		},
		{
			name: "distribution name wins over metadata name",
			cfg:  metaCluster("meta-name"),
			distCfg: &clusterprovisioner.DistributionConfig{
				Kind: &v1alpha4.Cluster{Name: "kind-name"},
			},
			want: "kind-name",
		},
		{
			name: "valid metadata name without distribution name",
			cfg:  metaCluster("meta-name"),
			want: "meta-name",
		},
		{
			name: "invalid metadata name describes nothing",
			cfg:  metaCluster("Invalid_Name"),
			want: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, resolveConfigClusterName(testCase.cfg, testCase.distCfg))
		})
	}
}

func metaCluster(name string) *v1alpha1.Cluster {
	cfg := &v1alpha1.Cluster{}
	cfg.Name = name

	return cfg
}
