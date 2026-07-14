package v1alpha1_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ExpandEnvVars drift guard.
//
// ExpandEnvVars (envvar.go) expands a hand-maintained list of Spec string
// fields. This guard walks the Spec type reflectively and asserts that every
// plain string field is either on the expanded list (verified behaviorally
// below) or on the documented skip list — so adding a new string field to the
// Spec forces an explicit expand-or-skip decision here.
// ---------------------------------------------------------------------------

// expandedSpecStringFields lists the Spec fields ExpandEnvVars expands,
// mirroring envvar.go. Paths are Go field paths relative to Spec; "[]" marks
// slice/array/map element scopes.
func expandedSpecStringFields() []string {
	return []string{
		"Editor",
		"Cluster.DistributionConfig",
		"Cluster.Connection.Kubeconfig",
		"Cluster.Connection.Context",
		"Cluster.LocalRegistry.Registry",
		"Cluster.Vanilla.MirrorsDir",
		"Cluster.Talos.Config",
		"Provider.Hetzner.SSHKeyName",
		"Provider.Hetzner.NetworkName",
		"Provider.Hetzner.PlacementGroup",
		"Workload.SourceDirectory",
		"Workload.Tag",
		"Workload.KustomizationFile",
		"Workload.Scan.Exceptions",
		"Chat.Model",
	}
}

// skippedSpecStringFields lists the Spec string fields ExpandEnvVars
// deliberately does not expand, grouped by reason. Removing a field from the
// Spec removes it here too; adding one requires choosing a list.
func skippedSpecStringFields() []string {
	return slices.Concat(
		skippedEnvVarNameFields(),
		skippedVersionPinFields(),
		skippedAutoscalerPoolFields(),
		skippedOIDCFields(),
		skippedClusterWorkloadConfigFields(),
		skippedProviderInfraFields(),
	)
}

// skippedEnvVarNameFields name environment variables themselves — expanding
// them would destroy the reference (resolution happens at use time).
func skippedEnvVarNameFields() []string {
	return []string{
		"Cluster.LocalRegistry.Credentials.CLITokenEnvVar",
		"Cluster.LocalRegistry.Credentials.ClusterTokenEnvVar",
		"Cluster.LocalRegistry.Credentials.TokenEnvVar",
		"Cluster.SOPS.AgeKeyEnvVar",
		"Cluster.SOPS.Env.Var",
		"Provider.AWS.AccessKeyIDEnvVar",
		"Provider.AWS.ProfileEnvVar",
		"Provider.AWS.RegionEnvVar",
		"Provider.AWS.SecretAccessKeyEnvVar",
		"Provider.AWS.SessionTokenEnvVar",
		"Provider.Azure.ResourceGroupEnvVar",
		"Provider.Azure.SubscriptionIDEnvVar",
		"Provider.GCP.LocationEnvVar",
		"Provider.GCP.ProjectEnvVar",
		"Provider.Hetzner.TokenEnvVar",
		"Provider.Kubernetes.ContextEnvVar",
		"Provider.Kubernetes.KubeconfigEnvVar",
		"Provider.Omni.EndpointEnvVar",
		"Provider.Omni.ServiceAccountKeyEnvVar",
	}
}

// skippedVersionPinFields are version pins and image identifiers resolved
// against registries; they have never been expanded.
func skippedVersionPinFields() []string {
	return []string{
		"Cluster.KubernetesVersion",
		"Cluster.Talos.KubernetesVersion",
		"Cluster.Talos.SchematicID",
		"Cluster.Talos.Version",
		"Provider.Omni.KubernetesVersion",
		"Provider.Omni.TalosVersion",
		"Workload.Flux.DistributionVersion",
		"Workload.Flux.OperatorVersion",
	}
}

// skippedAutoscalerPoolFields are autoscaler node-pool definitions (names,
// server types, locations, labels, taints) passed verbatim to the
// cluster-autoscaler configuration.
func skippedAutoscalerPoolFields() []string {
	return []string{
		"Cluster.Autoscaler.Node.Pools[].Labels[]",
		"Cluster.Autoscaler.Node.Pools[].Location",
		"Cluster.Autoscaler.Node.Pools[].Name",
		"Cluster.Autoscaler.Node.Pools[].ServerType",
		"Cluster.Autoscaler.Node.Pools[].Taints[].Key",
		"Cluster.Autoscaler.Node.Pools[].Taints[].Value",
		"Cluster.Autoscaler.Node.ScaleDownUnneededTime",
		"Cluster.Autoscaler.Node.ScaleDownUtilizationThreshold",
		"Provider.Hetzner.AutoscalerNodePoolNames[]",
		"Provider.Hetzner.AutoscalerNodePools[].Labels[]",
		"Provider.Hetzner.AutoscalerNodePools[].Location",
		"Provider.Hetzner.AutoscalerNodePools[].Name",
		"Provider.Hetzner.AutoscalerNodePools[].ServerType",
		"Provider.Hetzner.AutoscalerNodePools[].Taints[].Key",
		"Provider.Hetzner.AutoscalerNodePools[].Taints[].Value",
	}
}

// skippedOIDCFields are OIDC issuer coordinates written into generated
// kubeconfig/apiserver configuration without expansion.
func skippedOIDCFields() []string {
	return []string{
		"Cluster.OIDC.CAFile",
		"Cluster.OIDC.ClientID",
		"Cluster.OIDC.ExtraScopes[]",
		"Cluster.OIDC.GroupsClaim",
		"Cluster.OIDC.GroupsPrefix",
		"Cluster.OIDC.IssuerURL",
		"Cluster.OIDC.UsernameClaim",
		"Cluster.OIDC.UsernamePrefix",
	}
}

// skippedClusterWorkloadConfigFields are remaining cluster/workload settings
// (enum-like strings, image lists, hooks, verification config) that the
// expansion list has never covered.
func skippedClusterWorkloadConfigFields() []string {
	return []string{
		"Chat.ReasoningEffort",
		"Cluster.ImportImages",
		"Cluster.SOPS.Extract.File",
		"Cluster.SOPS.Extract.PublicKeys[]",
		"Cluster.Talos.Extensions[]",
		"Cluster.Talos.ExtraPortMappings[].Protocol",
		"Workload.Flux.Verify.MatchOIDCIdentity[].Issuer",
		"Workload.Flux.Verify.MatchOIDCIdentity[].Subject",
		"Workload.Flux.Verify.Provider",
		"Workload.Flux.Verify.SecretRef.Name",
		"Workload.Scan.Frameworks[]",
		"Workload.Validation.Rules",
		"Workload.Validation.SchemaLocations[]",
		"Workload.Validation.SkipKinds[]",
		"Workload.Watch.Hooks[]",
	}
}

// skippedProviderInfraFields are provider infrastructure coordinates (server
// types, locations, CIDRs, endpoints) consumed verbatim by the providers.
func skippedProviderInfraFields() []string {
	return []string{
		"Provider.Hetzner.AllowedCIDRs[]",
		"Provider.Hetzner.ControlPlaneServerType",
		"Provider.Hetzner.FallbackLocations[]",
		"Provider.Hetzner.FloatingIPLocation",
		"Provider.Hetzner.Location",
		"Provider.Hetzner.NetworkCIDR",
		"Provider.Hetzner.WorkerServerType",
		"Provider.Kubernetes.Context",
		"Provider.Kubernetes.GatewayClassName",
		"Provider.Kubernetes.Kubeconfig",
		"Provider.Kubernetes.Persistence.Size",
		"Provider.Kubernetes.Persistence.StorageClassName",
		"Provider.Kubernetes.PodCIDR",
		"Provider.Kubernetes.ServiceCIDR",
		"Provider.Omni.Endpoint",
		"Provider.Omni.MachineClass",
		"Provider.Omni.Machines[]",
	}
}

// TestExpandEnvVars_CoversEverySpecStringField asserts the expanded and skip
// lists exactly partition the set of plain string fields reachable from Spec.
func TestExpandEnvVars_CoversEverySpecStringField(t *testing.T) {
	t.Parallel()

	discovered := collectPlainStringFieldPaths(reflect.TypeFor[v1alpha1.Spec](), "")
	slices.Sort(discovered)

	accounted := slices.Concat(expandedSpecStringFields(), skippedSpecStringFields())
	slices.Sort(accounted)

	for _, path := range discovered {
		assert.Contains(
			t, accounted, path,
			"Spec string field %q is neither expanded by ExpandEnvVars nor on the documented "+
				"skip list; add it to envvar.go + expandedSpecStringFields, or document it in "+
				"skippedSpecStringFields", path,
		)
	}

	for _, path := range accounted {
		assert.Contains(
			t, discovered, path,
			"listed field %q no longer exists as a plain string field on Spec; "+
				"remove it from the expanded/skip lists (and envvar.go if expanded)", path,
		)
	}
}

// collectPlainStringFieldPaths returns the Go field paths of every plain
// (unnamed-type) string field reachable from the given type. Named string
// types (enums) are excluded; slice, array, and map value scopes are marked
// with "[]".
func collectPlainStringFieldPaths(typ reflect.Type, prefix string) []string {
	//nolint:exhaustive // Default case handles all other reflect.Kind types.
	switch typ.Kind() {
	case reflect.String:
		// Only unnamed string fields hold user-supplied expandable text; named
		// string types are enums with fixed value sets.
		if typ.PkgPath() == "" {
			return []string{prefix}
		}

		return nil
	case reflect.Pointer:
		return collectPlainStringFieldPaths(typ.Elem(), prefix)
	case reflect.Slice, reflect.Array, reflect.Map:
		return collectPlainStringFieldPaths(typ.Elem(), prefix+"[]")
	case reflect.Struct:
		var paths []string

		for field := range typ.Fields() {
			if !field.IsExported() {
				continue
			}

			fieldPrefix := field.Name
			if prefix != "" {
				fieldPrefix = prefix + "." + field.Name
			}

			paths = append(paths, collectPlainStringFieldPaths(field.Type, fieldPrefix)...)
		}

		return paths
	default:
		return nil
	}
}

// TestExpandEnvVars_ExpandsListedFields behaviorally verifies the expanded
// list: every listed field set to a placeholder must come back expanded.
func TestExpandEnvVars_ExpandsListedFields(t *testing.T) {
	// t.Setenv forbids t.Parallel().
	t.Setenv("KSAIL_EXPAND_PROBE", "probe-value")

	cluster := &v1alpha1.Cluster{}
	specValue := reflect.ValueOf(cluster).Elem().FieldByName("Spec")

	for _, path := range expandedSpecStringFields() {
		fieldByPath(t, specValue, path).SetString("${KSAIL_EXPAND_PROBE}")
	}

	cluster.ExpandEnvVars()

	for _, path := range expandedSpecStringFields() {
		assert.Equal(
			t, "probe-value", fieldByPath(t, specValue, path).String(),
			"field %q is on the expanded list but was not expanded; update envvar.go", path,
		)
	}
}

// fieldByPath resolves a dot-separated Go field path on a struct value.
func fieldByPath(t *testing.T, value reflect.Value, path string) reflect.Value {
	t.Helper()

	current := value

	for name := range strings.SplitSeq(path, ".") {
		current = current.FieldByName(name)
		require.True(t, current.IsValid(), "field path %q does not resolve", path)
	}

	return current
}
