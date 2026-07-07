package clusterautoscalerinstaller_test

import (
	"context"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	clusterautoscalerinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/clusterautoscaler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const (
	envHCLOUDPublicIPv4 = "HCLOUD_PUBLIC_IPV4"
	envHCLOUDPublicIPv6 = "HCLOUD_PUBLIC_IPV6"
)

// coreInformerReadVerbs is the get/list/watch verb set the autoscaler's core
// Deployment/ResourceQuota informers require (ksail#5405).
func coreInformerReadVerbs() []string {
	return []string{"get", "list", "watch"}
}

// renderedRBAC mirrors the rbac.additionalRules shape of the rendered chart
// values so a test can assert the full PolicyRule (apiGroups + resources +
// verbs), not just resource-name substrings.
type renderedRBAC struct {
	RBAC struct {
		AdditionalRules []struct {
			APIGroups []string `json:"apiGroups"`
			Resources []string `json:"resources"`
			Verbs     []string `json:"verbs"`
		} `json:"additionalRules"`
	} `json:"rbac"`
}

// assertHasRBACRule unmarshals the rendered chart values and asserts an
// rbac.additionalRules entry matches the given apiGroups, resources, and verbs
// EXACTLY — so a wrong apiGroups or a missing verb fails the test (ksail#5405).
func assertHasRBACRule(t *testing.T, valuesYaml string, apiGroups, resources, verbs []string) {
	t.Helper()

	var rendered renderedRBAC
	require.NoError(t, yaml.Unmarshal([]byte(valuesYaml), &rendered))

	for _, rule := range rendered.RBAC.AdditionalRules {
		if assert.ObjectsAreEqual(apiGroups, rule.APIGroups) &&
			assert.ObjectsAreEqual(resources, rule.Resources) &&
			assert.ObjectsAreEqual(verbs, rule.Verbs) {
			return
		}
	}

	t.Errorf(
		"no rbac.additionalRules entry matched apiGroups=%v resources=%v verbs=%v; got %+v",
		apiGroups, resources, verbs, rendered.RBAC.AdditionalRules,
	)
}

func TestNewInstaller(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer, err := clusterautoscalerinstaller.NewInstaller(
		client, 5*time.Minute, v1alpha1.NodeAutoscalerConfig{}, false, true, true,
	)
	require.NoError(t, err)

	assert.NotNil(t, installer)
}

func TestNewInstaller_HAEnabled(t *testing.T) {
	t.Parallel()

	client := helm.NewMockInterface(t)
	installer, err := clusterautoscalerinstaller.NewInstaller(
		client, 5*time.Minute, v1alpha1.NodeAutoscalerConfig{}, true, true, true,
	)
	require.NoError(t, err)
	require.NotNil(t, installer)
}

func TestClusterAutoscalerInstaller_ValuesYaml_HA(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		haEnabled bool
		assertFn  func(t *testing.T, valuesYaml string)
	}{
		{
			name:      "HAEnabled",
			haEnabled: true,
			assertFn: func(t *testing.T, valuesYaml string) {
				t.Helper()
				assert.Contains(t, valuesYaml, "replicas: 2",
					"ValuesYaml should contain replicas: 2 when HA is enabled")
			},
		},
		{
			name:      "HADisabled",
			haEnabled: false,
			assertFn: func(t *testing.T, valuesYaml string) {
				t.Helper()
				assert.NotContains(t, valuesYaml, "replicas: 2",
					"ValuesYaml should not contain replicas: 2 when HA is disabled")
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertHAValuesYaml(t, test.haEnabled, test.assertFn)
		})
	}
}

func assertHAValuesYaml(
	t *testing.T,
	haEnabled bool,
	assertFn func(t *testing.T, valuesYaml string),
) {
	t.Helper()

	cfg := v1alpha1.NodeAutoscalerConfig{
		Pools: []v1alpha1.NodePool{
			{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
		},
		Expander: v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
	}

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assertFn(t, spec.ValuesYaml)

				return true
			}),
		).
		Return(nil, nil)

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client, 5*time.Second, cfg, haEnabled, true, true,
	)
	require.NoError(t, err)

	err = installer.Install(context.Background())
	require.NoError(t, err)
}

func TestClusterAutoscalerInstaller_PublicNetExtraEnv(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		ipv4        bool
		ipv6        bool
		wantContain []string
		wantOmit    []string
	}{
		{
			name:     "BothEnabledOmitsPublicNetEnv",
			ipv4:     true,
			ipv6:     true,
			wantOmit: []string{envHCLOUDPublicIPv4, envHCLOUDPublicIPv6},
		},
		{
			name:        "IPv4DisabledKeepsIPv6Default",
			ipv4:        false,
			ipv6:        true,
			wantContain: []string{envHCLOUDPublicIPv4},
			wantOmit:    []string{envHCLOUDPublicIPv6},
		},
		{
			name:        "BothDisabled",
			ipv4:        false,
			ipv6:        false,
			wantContain: []string{envHCLOUDPublicIPv4, envHCLOUDPublicIPv6},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertPublicNetValuesYaml(t, test.ipv4, test.ipv6, test.wantContain, test.wantOmit)
		})
	}
}

func assertPublicNetValuesYaml(
	t *testing.T,
	ipv4, ipv6 bool,
	wantContain, wantOmit []string,
) {
	t.Helper()

	cfg := v1alpha1.NodeAutoscalerConfig{
		Pools: []v1alpha1.NodePool{
			{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
		},
		Expander: v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
	}

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				for _, want := range wantContain {
					assert.Contains(t, spec.ValuesYaml, want)
				}

				for _, omit := range wantOmit {
					assert.NotContains(t, spec.ValuesYaml, omit)
				}

				return true
			}),
		).
		Return(nil, nil)

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client,
		5*time.Second,
		cfg,
		false,
		ipv4,
		ipv6,
	)
	require.NoError(t, err)

	require.NoError(t, installer.Install(context.Background()))
}

func TestClusterAutoscalerInstaller_InstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	expectInstall(t, client, nil)

	err := installer.Install(context.Background())

	require.NoError(t, err)
}

func TestClusterAutoscalerInstaller_InstallError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	expectInstall(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to install cluster-autoscaler")
}

func TestClusterAutoscalerInstaller_InstallAddRepositoryError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, assert.AnError)

	err := installer.Install(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to add autoscaler repository")
}

func TestClusterAutoscalerInstaller_UninstallSuccess(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "cluster-autoscaler", "kube-system").
		Return(nil)

	err := installer.Uninstall(context.Background())

	require.NoError(t, err)
}

func TestClusterAutoscalerInstaller_UninstallError(t *testing.T) {
	t.Parallel()

	installer, client := newInstallerWithDefaults(t)
	client.EXPECT().
		UninstallRelease(mock.Anything, "cluster-autoscaler", "kube-system").
		Return(assert.AnError)

	err := installer.Uninstall(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to uninstall cluster-autoscaler")
}

// TestClusterAutoscalerInstaller_ValuesYaml_NodePools verifies that pool fields
// are rendered correctly in the Helm values YAML.
func TestClusterAutoscalerInstaller_ValuesYaml_NodePools(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NodeAutoscalerConfig{
		Pools: []v1alpha1.NodePool{
			{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
			{Name: "highmem", ServerType: "cax41", Location: "nbg1", Min: 0, Max: 3},
		},
		Expander: v1alpha1.AutoscalerExpanderList{
			v1alpha1.AutoscalerExpanderLeastWaste,
		},
		ScaleDownUnneededTime: "10m",
	}

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Contains(t, spec.ValuesYaml, "name: workers")
				assert.Contains(t, spec.ValuesYaml, "instanceType: cx23")
				assert.Contains(t, spec.ValuesYaml, "region: fsn1")
				assert.Contains(t, spec.ValuesYaml, "minSize: 1")
				assert.Contains(t, spec.ValuesYaml, "maxSize: 5")
				assert.Contains(t, spec.ValuesYaml, "name: highmem")
				assert.Contains(t, spec.ValuesYaml, "instanceType: cax41")
				assert.Contains(t, spec.ValuesYaml, "region: nbg1")
				assert.Contains(t, spec.ValuesYaml, "maxSize: 3")

				return true
			}),
		).
		Return(nil, nil)

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client,
		5*time.Second,
		cfg,
		false,
		true,
		true,
	)
	require.NoError(t, err)

	err = installer.Install(context.Background())
	require.NoError(t, err)
}

// TestClusterAutoscalerInstaller_ValuesYaml_Expanders verifies expander-to-chart
// value mappings for all supported enum values.
func TestClusterAutoscalerInstaller_ValuesYaml_Expanders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		expander      v1alpha1.AutoscalerExpanderList
		wantHelmValue string
	}{
		{
			name:          "least_waste_expander",
			expander:      v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
			wantHelmValue: "least-waste",
		},
		{
			name:          "price_expander",
			expander:      v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderPrice},
			wantHelmValue: "price",
		},
		{
			name:          "least_nodes_expander",
			expander:      v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastNodes},
			wantHelmValue: "least-nodes",
		},
		{
			name:          "random_expander",
			expander:      v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderRandom},
			wantHelmValue: "random",
		},
		{
			name: "priority_list_joined_with_commas",
			expander: v1alpha1.AutoscalerExpanderList{
				v1alpha1.AutoscalerExpanderLeastNodes,
				v1alpha1.AutoscalerExpanderLeastWaste,
			},
			wantHelmValue: "least-nodes,least-waste",
		},
		{
			name:          "empty_expander_defaults_to_least_waste",
			expander:      v1alpha1.AutoscalerExpanderList{},
			wantHelmValue: "least-waste",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			runExpanderTest(t, testCase.expander, testCase.wantHelmValue)
		})
	}
}

// runExpanderTest creates an installer with the given expander and asserts that
// the rendered ValuesYaml contains wantHelmValue.
func runExpanderTest(
	t *testing.T,
	expander v1alpha1.AutoscalerExpanderList,
	wantHelmValue string,
) {
	t.Helper()

	cfg := v1alpha1.NodeAutoscalerConfig{Expander: expander}
	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Contains(
					t,
					spec.ValuesYaml,
					wantHelmValue,
					"ValuesYaml should contain the expander value",
				)

				return true
			}),
		).
		Return(nil, nil)

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client,
		5*time.Second,
		cfg,
		false,
		true,
		true,
	)
	require.NoError(t, err)
	require.NotNil(t, installer)

	err = installer.Install(context.Background())
	require.NoError(t, err)
}

// assertValuesYamlContents asserts that the rendered cluster-autoscaler values
// YAML contains every required section and key field.
func assertValuesYamlContents(t *testing.T, valuesYaml string) {
	t.Helper()

	required := []string{
		"cloudProvider: hetzner",
		"autoscalingGroups:",
		"name: pool1",
		"instanceType: cx23",
		"region: fsn1",
		"expander: least-waste",
		"scale-down-unneeded-time: 15m",
		"max-nodes-total: 10",
		"scale-down-delay-after-add: 5m",
		"scale-down-delay-after-delete: 2m",
		"extraEnvSecrets:",
		"HCLOUD_TOKEN:",
		"HCLOUD_NETWORK:",
		"HCLOUD_CLUSTER_CONFIG:",
		"cluster-autoscaler-config",
		"tolerations:",
		"node-role.kubernetes.io/control-plane",
		"nodeSelector:",
		"rbac:",
		"resources:",
	}
	for _, want := range required {
		assert.Contains(t, valuesYaml, want)
	}

	// Core-informer RBAC rules the autoscaler binary needs unconditionally,
	// granted even without capacity-buffers — assert the full rule shape so a
	// wrong apiGroups or missing verb is caught, not just the resource name
	// (ksail#5405).
	assertHasRBACRule(
		t, valuesYaml, []string{"apps"}, []string{"deployments"}, coreInformerReadVerbs(),
	)
	assertHasRBACRule(
		t, valuesYaml, []string{""}, []string{"resourcequotas"}, coreInformerReadVerbs(),
	)
}

// TestClusterAutoscalerInstaller_ValuesYaml_Contents verifies that the rendered
// YAML contains all required sections and key fields by inspecting the ChartSpec
// via mock assertions.
func TestClusterAutoscalerInstaller_ValuesYaml_Contents(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NodeAutoscalerConfig{
		Enabled: v1alpha1.NodeAutoscalerEnabledEnabled,
		Pools: []v1alpha1.NodePool{
			{Name: "pool1", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 3},
		},
		Expander: v1alpha1.AutoscalerExpanderList{
			v1alpha1.AutoscalerExpanderLeastWaste,
		},
		MaxNodesTotal:         10,
		ScaleDownUnneededTime: "15m",
	}

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "cluster-autoscaler", spec.ReleaseName)
				assert.Equal(t, "autoscaler/cluster-autoscaler", spec.ChartName)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(t, "https://kubernetes.github.io/autoscaler", spec.RepoURL)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)

				assertValuesYamlContents(t, spec.ValuesYaml)

				return true
			}),
		).
		Return(nil, nil)

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client,
		5*time.Second,
		cfg,
		false,
		true,
		true,
	)
	require.NoError(t, err)
	err = installer.Install(context.Background())
	require.NoError(t, err)
}

// TestClusterAutoscalerInstaller_ValuesYaml_DefaultScaleDownTime verifies that
// an empty ScaleDownUnneededTime defaults to "10m".
func TestClusterAutoscalerInstaller_ValuesYaml_DefaultScaleDownTime(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NodeAutoscalerConfig{
		ScaleDownUnneededTime: "",
	}

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Contains(
					t,
					spec.ValuesYaml,
					"scale-down-unneeded-time: 10m",
					"expected default scale-down-unneeded-time 10m",
				)

				return true
			}),
		).
		Return(nil, nil)

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client,
		5*time.Second,
		cfg,
		false,
		true,
		true,
	)
	require.NoError(t, err)
	err = installer.Install(context.Background())
	require.NoError(t, err)
}

// TestClusterAutoscalerInstaller_ValuesYaml_MaxNodesTotalOmittedWhenZero verifies
// that max-nodes-total is omitted from extraArgs when MaxNodesTotal is 0.
func TestClusterAutoscalerInstaller_ValuesYaml_MaxNodesTotalOmittedWhenZero(t *testing.T) {
	t.Parallel()

	cfg := v1alpha1.NodeAutoscalerConfig{
		MaxNodesTotal: 0,
	}

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.NotContains(
					t,
					spec.ValuesYaml,
					"max-nodes-total",
					"max-nodes-total should be omitted when MaxNodesTotal=0",
				)

				return true
			}),
		).
		Return(nil, nil)

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client,
		5*time.Second,
		cfg,
		false,
		true,
		true,
	)
	require.NoError(t, err)
	err = installer.Install(context.Background())
	require.NoError(t, err)
}

// TestClusterAutoscalerInstaller_ValuesYaml_IgnoreDaemonsetsUtilization verifies
// that the ignore-daemonsets-utilization extraArg is rendered only when the knob
// is enabled, and omitted (upstream default) otherwise so existing releases see
// no values drift.
func TestClusterAutoscalerInstaller_ValuesYaml_IgnoreDaemonsetsUtilization(t *testing.T) {
	t.Parallel()

	t.Run("enabled renders the flag", func(t *testing.T) {
		t.Parallel()
		assertAutoscalerValuesYaml(t,
			v1alpha1.NodeAutoscalerConfig{IgnoreDaemonsetsUtilization: true},
			[]string{"ignore-daemonsets-utilization: true"}, nil)
	})

	t.Run("disabled omits the flag", func(t *testing.T) {
		t.Parallel()
		assertAutoscalerValuesYaml(t,
			v1alpha1.NodeAutoscalerConfig{IgnoreDaemonsetsUtilization: false},
			nil, []string{"ignore-daemonsets-utilization"})
	})
}

// TestClusterAutoscalerInstaller_ValuesYaml_ScaleDownUtilizationThreshold verifies
// that the scale-down-utilization-threshold extraArg is rendered verbatim only when
// set, and omitted (upstream default 0.5) when empty so existing releases see no
// values drift.
func TestClusterAutoscalerInstaller_ValuesYaml_ScaleDownUtilizationThreshold(t *testing.T) {
	t.Parallel()

	t.Run("set renders the flag", func(t *testing.T) {
		t.Parallel()
		assertAutoscalerValuesYaml(t,
			v1alpha1.NodeAutoscalerConfig{ScaleDownUtilizationThreshold: "0.7"},
			[]string{"scale-down-utilization-threshold: \"0.7\""}, nil)
	})

	t.Run("empty omits the flag", func(t *testing.T) {
		t.Parallel()
		assertAutoscalerValuesYaml(t,
			v1alpha1.NodeAutoscalerConfig{ScaleDownUtilizationThreshold: ""},
			nil, []string{"scale-down-utilization-threshold"})
	})
}

// assertAutoscalerValuesYaml installs with cfg and asserts the rendered chart
// ValuesYaml contains every wantContain substring and none of the wantOmit ones.
func assertAutoscalerValuesYaml(
	t *testing.T,
	cfg v1alpha1.NodeAutoscalerConfig,
	wantContain, wantOmit []string,
) {
	t.Helper()

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(mock.Anything, mock.MatchedBy(func(spec *helm.ChartSpec) bool {
			for _, want := range wantContain {
				assert.Contains(t, spec.ValuesYaml, want)
			}

			for _, omit := range wantOmit {
				assert.NotContains(t, spec.ValuesYaml, omit)
			}

			return true
		})).
		Return(nil, nil)

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client, 5*time.Second, cfg, false, true, true,
	)
	require.NoError(t, err)
	require.NoError(t, installer.Install(context.Background()))
}

// TestClusterAutoscalerInstaller_ValuesYaml_SkipNodesFlags verifies that the
// skip-nodes-with-local-storage / skip-nodes-with-system-pods pointer knobs are
// omitted when nil (inheriting the upstream default true) and rendered verbatim
// when set to an explicit true or false.
func TestClusterAutoscalerInstaller_ValuesYaml_SkipNodesFlags(t *testing.T) {
	t.Parallel()

	ptr := func(b bool) *bool { return &b }

	tests := []struct {
		name                   string
		localStore, systemPods *bool
		wantContain, wantOmit  []string
	}{
		{
			name:     "nil omits both flags",
			wantOmit: []string{"skip-nodes-with-local-storage", "skip-nodes-with-system-pods"},
		},
		{
			name:       "explicit false renders both",
			localStore: ptr(false),
			systemPods: ptr(false),
			wantContain: []string{
				"skip-nodes-with-local-storage: false",
				"skip-nodes-with-system-pods: false",
			},
		},
		{
			name:       "explicit true renders both",
			localStore: ptr(true),
			systemPods: ptr(true),
			wantContain: []string{
				"skip-nodes-with-local-storage: true",
				"skip-nodes-with-system-pods: true",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertAutoscalerValuesYaml(t, v1alpha1.NodeAutoscalerConfig{
				SkipNodesWithLocalStorage: test.localStore,
				SkipNodesWithSystemPods:   test.systemPods,
			}, test.wantContain, test.wantOmit)
		})
	}
}

// TestClusterAutoscalerInstaller_ValuesYaml_CapacityBuffers verifies that
// enabling capacityBuffers renders the two feature flags, the buffer
// controller's RBAC rules (CapacityBuffer CRs plus the Deployment and
// ResourceQuota read access its informers require), and the embedded
// CapacityBuffer CRD via extraObjects — and that all of them are omitted from
// the values when the feature is disabled.
func TestClusterAutoscalerInstaller_ValuesYaml_CapacityBuffers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		enabled     bool
		wantContain []string
		wantOmit    []string
	}{
		{
			name:    "EnabledRendersFlagsRBACAndCRD",
			enabled: true,
			wantContain: []string{
				"capacity-buffer-controller-enabled: true",
				"capacity-buffer-pod-injection-enabled: true",
				// JSON encoding is required so the CapacityBuffer CRD client works
				// (the autoscaler defaults to protobuf, which CRDs cannot satisfy) —
				// ksail#5603.
				"kube-api-content-type: application/json",
				"additionalRules:",
				"capacitybuffers",
				"- deployments",
				"- resourcequotas",
				"kind: CustomResourceDefinition",
				"name: capacitybuffers.autoscaling.x-k8s.io",
			},
		},
		{
			name:    "DisabledOmitsFlagsAndCRD",
			enabled: false,
			wantContain: []string{
				// Core-informer rules are granted unconditionally, even with
				// capacity-buffers disabled (ksail#5405).
				"additionalRules:",
				"- deployments",
				"- resourcequotas",
			},
			wantOmit: []string{
				"capacity-buffer-controller-enabled",
				"capacity-buffer-pod-injection-enabled",
				"kube-api-content-type",
				"capacitybuffers",
				"kind: CustomResourceDefinition",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertCapacityBuffersValuesYaml(t, test.enabled, test.wantContain, test.wantOmit)
		})
	}
}

func assertCapacityBuffersValuesYaml(
	t *testing.T,
	enabled bool,
	wantContain, wantOmit []string,
) {
	t.Helper()

	cfg := v1alpha1.NodeAutoscalerConfig{
		Pools: []v1alpha1.NodePool{
			{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
		},
		Expander:        v1alpha1.AutoscalerExpanderList{v1alpha1.AutoscalerExpanderLeastWaste},
		CapacityBuffers: enabled,
	}

	client := helm.NewMockInterface(t)
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				for _, want := range wantContain {
					assert.Contains(t, spec.ValuesYaml, want)
				}

				for _, omit := range wantOmit {
					assert.NotContains(t, spec.ValuesYaml, omit)
				}

				// Core-informer rules are granted unconditionally; assert their
				// full shape regardless of the capacity-buffers toggle (ksail#5405).
				assertHasRBACRule(t, spec.ValuesYaml,
					[]string{"apps"}, []string{"deployments"}, coreInformerReadVerbs())
				assertHasRBACRule(t, spec.ValuesYaml,
					[]string{""}, []string{"resourcequotas"}, coreInformerReadVerbs())

				if enabled {
					assertHasRBACRule(t, spec.ValuesYaml,
						[]string{"autoscaling.x-k8s.io"},
						[]string{"capacitybuffers", "capacitybuffers/status"},
						[]string{"get", "list", "watch", "update", "patch"})
				}

				return true
			}),
		).
		Return(nil, nil)

	installer, err := clusterautoscalerinstaller.NewInstaller(
		client, 5*time.Second, cfg, false, true, true,
	)
	require.NoError(t, err)

	require.NoError(t, installer.Install(context.Background()))
}

func newInstallerWithDefaults(t *testing.T) (
	*clusterautoscalerinstaller.Installer,
	*helm.MockInterface,
) {
	t.Helper()

	client := helm.NewMockInterface(t)
	cfg := v1alpha1.NodeAutoscalerConfig{
		Pools: []v1alpha1.NodePool{
			{Name: "workers", ServerType: "cx23", Location: "fsn1", Min: 1, Max: 5},
		},
		Expander: v1alpha1.AutoscalerExpanderList{
			v1alpha1.AutoscalerExpanderLeastWaste,
		},
		ScaleDownUnneededTime: "10m",
	}
	installer, err := clusterautoscalerinstaller.NewInstaller(
		client,
		5*time.Second,
		cfg,
		false,
		true,
		true,
	)
	require.NoError(t, err)

	return installer, client
}

func expectAddRepository(t *testing.T, client *helm.MockInterface, err error) {
	t.Helper()
	client.EXPECT().
		AddRepository(
			mock.Anything,
			mock.MatchedBy(func(entry *helm.RepositoryEntry) bool {
				assert.Equal(t, "autoscaler", entry.Name)
				assert.Equal(t, "https://kubernetes.github.io/autoscaler", entry.URL)

				return true
			}),
			mock.Anything,
		).
		Return(err)
}

func expectInstall(t *testing.T, client *helm.MockInterface, installErr error) {
	t.Helper()
	client.EXPECT().
		GetReleaseStorageLabels(mock.Anything, mock.Anything, mock.Anything).
		Return(nil, nil)
	expectAddRepository(t, client, nil)
	client.EXPECT().
		InstallOrUpgradeChart(
			mock.Anything,
			mock.MatchedBy(func(spec *helm.ChartSpec) bool {
				assert.Equal(t, "cluster-autoscaler", spec.ReleaseName)
				assert.Equal(t, "autoscaler/cluster-autoscaler", spec.ChartName)
				assert.Equal(t, "kube-system", spec.Namespace)
				assert.Equal(t, "https://kubernetes.github.io/autoscaler", spec.RepoURL)
				assert.True(t, spec.Atomic)
				assert.True(t, spec.Wait)
				assert.True(t, spec.WaitForJobs)

				return true
			}),
		).
		Return(nil, installErr)
}
