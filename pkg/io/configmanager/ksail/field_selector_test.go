package configmanager_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type standardFieldSelectorCase struct {
	name            string
	factory         func() configmanager.FieldSelector[v1alpha1.Cluster]
	expectedDesc    string
	expectedDefault any
	assertPointer   func(*testing.T, *v1alpha1.Cluster, any)
}

type defaultClusterSelectorCase struct {
	name            string
	selector        configmanager.FieldSelector[v1alpha1.Cluster]
	expectedDefault any
	assertField     func(*testing.T, any)
}

//nolint:funlen // Table-driven cases are verbose; keep assertions straightforward.
func TestStandardFieldSelectors(t *testing.T) {
	t.Parallel()

	cases := []standardFieldSelectorCase{
		{
			name:            "distribution",
			factory:         configmanager.DefaultDistributionFieldSelector,
			expectedDesc:    "Kubernetes distribution to use",
			expectedDefault: v1alpha1.DistributionVanilla,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.Distribution)
			},
		},
		{
			name:            "source directory",
			factory:         configmanager.StandardSourceDirectoryFieldSelector,
			expectedDesc:    "Directory containing workloads to deploy",
			expectedDefault: "k8s",
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Workload.SourceDirectory)
			},
		},
		{
			name:            "distribution config",
			factory:         configmanager.DefaultDistributionConfigFieldSelector,
			expectedDesc:    "Configuration file for the distribution",
			expectedDefault: "",
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.DistributionConfig)
			},
		},
		{
			name:            "context",
			factory:         configmanager.DefaultContextFieldSelector,
			expectedDesc:    "Kubernetes context of cluster",
			expectedDefault: nil,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.Connection.Context)
			},
		},
		{
			name:            "cni",
			factory:         configmanager.DefaultCNIFieldSelector,
			expectedDesc:    "Container Network Interface (CNI) to use",
			expectedDefault: v1alpha1.CNIDefault,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.CNI)
			},
		},
		{
			name:    "gitops engine",
			factory: configmanager.DefaultGitOpsEngineFieldSelector,
			expectedDesc: "GitOps engine to use (None disables GitOps, " +
				"Flux installs Flux controllers, " +
				"ArgoCD installs Argo CD)",
			expectedDefault: v1alpha1.GitOpsEngineNone,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.GitOpsEngine)
			},
		},
		{
			name:    "local registry",
			factory: configmanager.DefaultLocalRegistryFieldSelector,
			expectedDesc: "Local registry specification: [user:pass@]host[:port][/path] " +
				"(e.g., localhost:5050, ghcr.io/myorg, ${USER}:${PASS}@ghcr.io:443/org)",
			expectedDefault: "",
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.LocalRegistry.Registry)
			},
		},
		{
			name:    "metrics server",
			factory: configmanager.DefaultMetricsServerFieldSelector,
			expectedDesc: "Metrics Server " +
				"(Default: use distribution, Enabled: install, Disabled: uninstall)",
			expectedDefault: v1alpha1.MetricsServerDefault,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.MetricsServer)
			},
		},
		{
			name:            "cert-manager",
			factory:         configmanager.DefaultCertManagerFieldSelector,
			expectedDesc:    "Cert-Manager configuration (Enabled: install, Disabled: skip)",
			expectedDefault: v1alpha1.CertManagerDisabled,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.CertManager)
			},
		},
		{
			name:            "csi",
			factory:         configmanager.DefaultCSIFieldSelector,
			expectedDesc:    "Container Storage Interface (Default: use distribution, Enabled: install CSI, Disabled: skip CSI)",
			expectedDefault: v1alpha1.CSIDefault,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.CSI)
			},
		},
		{
			name:            "provider",
			factory:         configmanager.DefaultProviderFieldSelector,
			expectedDesc:    "Infrastructure provider backend (e.g., Docker)",
			expectedDefault: v1alpha1.ProviderDocker,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.Provider)
			},
		},
		{
			name:    "load balancer",
			factory: configmanager.DefaultLoadBalancerFieldSelector,
			expectedDesc: "LoadBalancer support " +
				"(Default: use distribution × provider, Enabled: install, Disabled: uninstall)",
			expectedDefault: v1alpha1.LoadBalancerDefault,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.LoadBalancer)
			},
		},
		{
			name:    "policy engine",
			factory: configmanager.DefaultPolicyEngineFieldSelector,
			expectedDesc: "Policy engine " +
				"(None: skip, Kyverno: install Kyverno, Gatekeeper: install Gatekeeper)",
			expectedDefault: v1alpha1.PolicyEngineNone,
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.PolicyEngine)
			},
		},
		{
			name:            "kubeconfig",
			factory:         configmanager.DefaultKubeconfigFieldSelector,
			expectedDesc:    "Path to kubeconfig file",
			expectedDefault: "~/.kube/config",
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.Connection.Kubeconfig)
			},
		},
		{
			name:            "control planes",
			factory:         configmanager.ControlPlanesFieldSelector,
			expectedDesc:    "Number of control-plane nodes",
			expectedDefault: int32(1),
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.Talos.ControlPlanes)
			},
		},
		{
			name:            "workers",
			factory:         configmanager.WorkersFieldSelector,
			expectedDesc:    "Number of worker nodes",
			expectedDefault: int32(0),
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.Talos.Workers)
			},
		},
		{
			name:    "import images",
			factory: configmanager.DefaultImportImagesFieldSelector,
			expectedDesc: "Path to tar archive with container images to import after cluster creation " +
				"but before component installation",
			expectedDefault: "",
			assertPointer: func(t *testing.T, cluster *v1alpha1.Cluster, ptr any) {
				t.Helper()
				assertPointerSame(t, ptr, &cluster.Spec.Cluster.ImportImages)
			},
		},
	}

	for _, testCase := range cases {
		caseData := testCase
		t.Run(caseData.name, func(t *testing.T) {
			t.Parallel()

			cluster := &v1alpha1.Cluster{}
			selector := caseData.factory()

			assert.Equal(t, caseData.expectedDesc, selector.Description)
			assert.Equal(t, caseData.expectedDefault, selector.DefaultValue)

			pointer := selector.Selector(cluster)
			caseData.assertPointer(t, cluster, pointer)
		})
	}
}

func assertPointerSame[T any](t *testing.T, actual any, expected *T) {
	t.Helper()

	value, ok := actual.(*T)
	require.True(t, ok)
	assert.Same(t, expected, value)
}

func TestDefaultClusterFieldSelectorsProvideDefaults(t *testing.T) {
	t.Parallel()

	selectors := configmanager.DefaultClusterFieldSelectors()
	require.Len(t, selectors, 6)

	cluster := v1alpha1.NewCluster()

	for _, selectorCase := range defaultClusterSelectorCases(selectors) {
		caseData := selectorCase
		t.Run(caseData.name, func(t *testing.T) {
			t.Parallel()

			field := caseData.selector.Selector(cluster)
			if caseData.expectedDefault != nil {
				assert.Equal(t, caseData.expectedDefault, caseData.selector.DefaultValue)
			}

			caseData.assertField(t, field)
		})
	}
}

//nolint:funlen // Explicit cases improve readability over indirect indexing.
func defaultClusterSelectorCases(
	selectors []configmanager.FieldSelector[v1alpha1.Cluster],
) []defaultClusterSelectorCase {
	return []defaultClusterSelectorCase{
		{
			name:            "distribution",
			selector:        selectors[0],
			expectedDefault: v1alpha1.DistributionVanilla,
			assertField: func(t *testing.T, field any) {
				t.Helper()

				_, ok := field.(*v1alpha1.Distribution)
				require.True(t, ok)
			},
		},
		{
			name:            "distribution config",
			selector:        selectors[1],
			expectedDefault: "",
			assertField: func(t *testing.T, field any) {
				t.Helper()

				_, ok := field.(*string)
				require.True(t, ok)
			},
		},
		{
			name:     "context",
			selector: selectors[2],
			assertField: func(t *testing.T, field any) {
				t.Helper()

				_, ok := field.(*string)
				require.True(t, ok)
			},
		},
		{
			name:            "kubeconfig",
			selector:        selectors[3],
			expectedDefault: "~/.kube/config",
			assertField: func(t *testing.T, field any) {
				t.Helper()

				_, ok := field.(*string)
				require.True(t, ok)
			},
		},
		{
			name:            "gitops engine",
			selector:        selectors[4],
			expectedDefault: v1alpha1.GitOpsEngineNone,
			assertField: func(t *testing.T, field any) {
				t.Helper()

				_, ok := field.(*v1alpha1.GitOpsEngine)
				require.True(t, ok)
			},
		},
		{
			name:            "local registry",
			selector:        selectors[5],
			expectedDefault: "",
			assertField: func(t *testing.T, field any) {
				t.Helper()

				_, ok := field.(*string)
				require.True(t, ok)
			},
		},
	}
}
