package v1alpha1_test

import (
	"bytes"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// ---------------------------------------------------------------------------
// Marshal-pipeline-move contract (Phase 4.1, item 2).
//
// The refactoring plan proposes moving the whole structToMap/convertByKind/prune
// pipeline OFF Cluster.MarshalJSON/MarshalYAML onto the single scaffolder write
// path, reverting operator/CRD serialization to plain struct tags. That move is
// only safe if it is byte-identical at the scaffolder write path and its wire
// impact on operator CR writes is understood. These tests pin both facts so the
// dedicated PR that performs the move has a ready regression gate:
//
//   1. plainClusterAlias drops the custom marshaller (the same trick
//      pkg/operator/api uses with fullCluster). Marshalling a pruned Cluster
//      through it reproduces the scaffolder YAML EXACTLY except for a trailing
//      `status: {}` line — so the write-path helper must continue to drop empty
//      nested structs (status), exactly as structToMap does today.
//
//   2. The operator/controller-runtime wire payload DOES change when the custom
//      marshaller is removed: the plain path emits un-pruned defaults and
//      `status: {}`. That is the wire change the plan flags for operator chart
//      e2e + a stored-CR upgrade test — it cannot be validated in unit tests
//      here, which is why item 2 ships separately.
// ---------------------------------------------------------------------------

// plainClusterAlias drops Cluster's custom MarshalJSON/MarshalYAML so YAML is
// produced from plain struct tags — the serialization the plan's move targets.
type plainClusterAlias v1alpha1.Cluster

func pipelineMoveSampleCluster() v1alpha1.Cluster {
	return v1alpha1.Cluster{
		TypeMeta:   metav1.TypeMeta{Kind: v1alpha1.Kind, APIVersion: v1alpha1.APIVersion},
		ObjectMeta: metav1.ObjectMeta{Name: "my-cluster", Namespace: "default"},
		Spec: v1alpha1.Spec{
			Editor: "vim",
			Cluster: v1alpha1.ClusterSpec{
				Distribution:  v1alpha1.DistributionTalos,
				Provider:      v1alpha1.ProviderHetzner,
				CNI:           v1alpha1.CNICilium,
				CSI:           v1alpha1.CSIEnabled,
				ControlPlanes: 3,
				Workers:       2,
				Talos:         v1alpha1.OptionsTalos{ISO: 999999},
				OIDC: v1alpha1.OIDCSpec{
					IssuerURL:     "https://dex.example.com",
					ClientID:      "kubectl",
					UsernameClaim: "sub",
				},
			},
			Provider: v1alpha1.ProviderSpec{
				Hetzner: v1alpha1.OptionsHetzner{Location: "nbg1", ServerLimit: 20},
			},
			Workload: v1alpha1.WorkloadSpec{SourceDirectory: "manifests", Tag: "v1.0"},
		},
	}
}

// TestMarshalPipelineMove_ScaffolderByteIdentityModuloStatus pins the exact diff
// between today's custom-marshaller YAML and a prune-then-plain-struct-tags YAML:
// they are identical except the plain path adds a trailing `status: {}` line.
// The dedicated move PR must keep the empty-status suppression to stay
// byte-identical at the scaffolder.
func TestMarshalPipelineMove_ScaffolderByteIdentityModuloStatus(t *testing.T) {
	t.Parallel()

	cluster := pipelineMoveSampleCluster()

	// Today's scaffolder path: sigs.k8s.io/yaml.Marshal -> Cluster.MarshalJSON -> prune.
	customYAML, err := yaml.Marshal(cluster)
	require.NoError(t, err)

	// Candidate new path: prune via the custom marshaller, decode into a plain
	// alias, then marshal with plain struct tags.
	prunedJSON, err := cluster.MarshalJSON()
	require.NoError(t, err)

	var plain plainClusterAlias

	require.NoError(t, yaml.Unmarshal(prunedJSON, &plain))

	plainYAML, err := yaml.Marshal(plain)
	require.NoError(t, err)

	// The only delta is the trailing empty-status line. Comparing raw bytes (via
	// bytes.Equal, not assert.Equal on two YAML strings) keeps the byte-level
	// contract without tripping testifylint's encoded-compare heuristic.
	trimmed := bytes.TrimSuffix(plainYAML, []byte("status: {}\n"))
	bytesMatch := bytes.Equal(customYAML, trimmed)
	assert.Truef(t, bytesMatch,
		"plain YAML must match custom YAML once trailing status is removed:\ncustom=%q\ntrimmed=%q",
		customYAML, trimmed)
	assert.NotEqual(t, len(plainYAML), len(trimmed),
		"plain marshalling must emit the trailing status line the custom marshaller drops")
}

// TestMarshalPipelineMove_OperatorWireChangesWithoutCustomMarshaller documents
// that dropping the custom marshaller changes the operator/controller-runtime
// wire payload (un-pruned defaults + status appear). This is the wire change the
// plan gates behind operator chart e2e; it is asserted here so the move PR
// cannot land believing the operator payload is unaffected.
func TestMarshalPipelineMove_OperatorWireChangesWithoutCustomMarshaller(t *testing.T) {
	t.Parallel()

	cluster := pipelineMoveSampleCluster()

	customJSON, err := cluster.MarshalJSON()
	require.NoError(t, err)

	plainJSON, err := yaml.Marshal(plainClusterAlias(cluster))
	require.NoError(t, err)

	// Custom marshaller prunes the Enabled CSI? No — CSI is non-default here, but
	// defaults like the empty status and zero-value scalar fields are pruned.
	// The plain path keeps status; the custom path drops it. They therefore differ,
	// which is the wire change to validate via e2e.
	assert.NotContains(t, string(customJSON), "status",
		"custom marshaller omits empty status")
	assert.Contains(t, string(plainJSON), "status",
		"plain struct-tag marshalling emits status — the operator wire delta")
}
