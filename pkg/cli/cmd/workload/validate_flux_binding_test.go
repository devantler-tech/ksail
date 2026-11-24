package workload_test

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTree writes files, keyed by slash-separated path relative to the tree
// root, and returns the root. Every folder holding a kustomization.yaml is a
// kustomization.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()

	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
		require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	}

	return root
}

// kustomizationOf is a kustomization.yaml listing resources.
func kustomizationOf(resources ...string) string {
	return "apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n  - " +
		strings.Join(resources, "\n  - ") + "\n"
}

// fluxKustomization is a Flux Kustomization from the tree's own flux-system
// source, applying path; extra is appended under spec.
func fluxKustomization(name, namespace, path, extra string) string {
	return `apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: ` + name + `
  namespace: ` + namespace + `
spec:
  interval: 10m
  prune: true
  path: ` + path + `
  sourceRef:
    kind: OCIRepository
    name: flux-system
    namespace: flux-system
` + extra
}

// replicaPatch is a Flux spec.patches entry setting the HelmRelease's
// replicaCount value.
func replicaPatch(count string) string {
	return `  patches:
    - target:
        kind: HelmRelease
        name: podinfo
      patch: |
        apiVersion: helm.toolkit.fluxcd.io/v2
        kind: HelmRelease
        metadata:
          name: podinfo
        spec:
          values:
            replicaCount: ` + count + `
`
}

// requireReplicasPolicy enforces at least two replicas on every HelmRelease.
const requireReplicasPolicy = `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-replicas
spec:
  validationFailureAction: Enforce
  rules:
    - name: at-least-two
      match:
        any:
          - resources:
              kinds:
                - HelmRelease
      validate:
        message: "HelmReleases must run at least two replicas"
        pattern:
          spec:
            values:
              replicaCount: ">=2"
`

// podinfoRelease is a shared base HelmRelease that sets no replicaCount: it is
// valid only once the Flux Kustomization applying it patches one in.
const podinfoRelease = `apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: podinfo
  namespace: podinfo
spec:
  interval: 10m
  chartRef:
    kind: OCIRepository
    name: podinfo
  values:
    image: ghcr.io/stefanprodan/podinfo
`

// onlyFluxControllerPolicy rejects ConfigMaps from anyone but Flux's own
// kustomize-controller, the shape of a tenant-restriction rule.
const onlyFluxControllerPolicy = `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: platform-only-configmaps
spec:
  validationFailureAction: Enforce
  rules:
    - name: only-platform
      match:
        any:
          - resources:
              kinds:
                - ConfigMap
      validate:
        message: "only the platform may apply ConfigMaps"
        deny:
          conditions:
            any:
              - key: "{{ request.userInfo.username }}"
                operator: NotEquals
                value: system:serviceaccount:flux-system:kustomize-controller
`

// configMap is a ConfigMap named name with no namespace of its own.
func configMap(name string) string {
	return "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " + name + "\ndata:\n  key: value\n"
}

// helmReleaseTree is a cluster folder whose Flux Kustomization applies the
// shared podinfo base with extra under its spec.
func helmReleaseTree(t *testing.T, extra string) string {
	t.Helper()

	return writeTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf("policy.yaml", "apps.yaml"),
		"clusters/prod/policy.yaml":        requireReplicasPolicy,
		"clusters/prod/apps.yaml": fluxKustomization(
			"apps",
			"flux-system",
			"./apps/podinfo",
			extra,
		),
		"apps/podinfo/kustomization.yaml": kustomizationOf("helmrelease.yaml"),
		"apps/podinfo/helmrelease.yaml":   podinfoRelease,
	})
}

// runBindingValidate validates root with the Kyverno pass on, Helm rendering
// off (the HelmRelease is the admitted object) and an empty local schema
// location, so no schema registry is contacted.
func runBindingValidate(t *testing.T, root string) (string, error) {
	t.Helper()

	return runValidate(t, root, "--kyverno-policies", "--skip-helm-render",
		"--schema-location-mode", "only", "--schema-location", t.TempDir())
}

// A HelmRelease base that fails on its own passes once the Flux patch that
// composes it is applied (devantler-tech/ksail#8509).
func TestValidateFluxBindingComposedHelmReleasePasses(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, helmReleaseTree(t, replicaPatch("2")))
	require.NoError(t, err, "the composed HelmRelease meets the policy")
}

// A real violation in the composed output still fails.
func TestValidateFluxBindingComposedViolationFails(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, helmReleaseTree(t, replicaPatch("1")))
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
	require.ErrorContains(t, err, `policy "require-replicas" rule "at-least-two" failed`)
	require.ErrorContains(t, err, "HelmRelease/podinfo/podinfo")
	require.ErrorContains(t, err, "applied by Flux Kustomization flux-system/apps")
}

// Without a Flux Kustomization the tree behaves as before: the base is
// evaluated as rendered and fails. This is the negative control for the
// composition test above.
func TestValidateFluxBindingAbsentEvaluatesBaseAsBefore(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"policies/kustomization.yaml":     kustomizationOf("policy.yaml"),
		"policies/policy.yaml":            requireReplicasPolicy,
		"apps/podinfo/kustomization.yaml": kustomizationOf("helmrelease.yaml"),
		"apps/podinfo/helmrelease.yaml":   podinfoRelease,
	})

	_, err := runBindingValidate(t, root)
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
	require.NotErrorIs(t, err, workload.ErrUnboundFluxKustomization)
}

// tenancyTree binds a platform folder to Flux's own identity and a tenant
// folder to the tenant's service account; both render a ConfigMap.
func tenancyTree(t *testing.T, extra map[string]string) string {
	t.Helper()

	files := map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf(
			"policy.yaml", "platform.yaml", "tenant.yaml",
		),
		"clusters/prod/policy.yaml":   onlyFluxControllerPolicy,
		"clusters/prod/platform.yaml": fluxKustomization("platform", "flux-system", "platform", ""),
		"clusters/prod/tenant.yaml": fluxKustomization("tenant", "tenant-a", "tenant",
			"  serviceAccountName: tenant-a\n  targetNamespace: tenant-a\n"),
		"platform/kustomization.yaml": kustomizationOf("configmap.yaml"),
		"platform/configmap.yaml":     configMap("platform-settings"),
		"tenant/kustomization.yaml":   kustomizationOf("configmap.yaml"),
		"tenant/configmap.yaml":       configMap("tenant-settings"),
	}

	maps.Copy(files, extra)

	return writeTree(t, files)
}

// A tenant-bound folder is rejected by a tenant-restriction rule while the
// platform-bound folder is admitted.
func TestValidateFluxBindingTenantRejectedPlatformAdmitted(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, tenancyTree(t, nil))
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
	require.ErrorContains(t, err, "ConfigMap/tenant-a/tenant-settings")
	require.ErrorContains(t, err, "as system:serviceaccount:tenant-a:tenant-a")
	assert.NotContains(t, err.Error(), "platform-settings", "the platform folder is admitted")
}

// The platform folder alone passes: Flux's own identity is admitted.
func TestValidateFluxBindingPlatformAdmitted(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf("policy.yaml", "platform.yaml"),
		"clusters/prod/policy.yaml":        onlyFluxControllerPolicy,
		"clusters/prod/platform.yaml": fluxKustomization(
			"platform", "flux-system", "platform", "",
		),
		"platform/kustomization.yaml": kustomizationOf("configmap.yaml"),
		"platform/configmap.yaml":     configMap("platform-settings"),
	})

	_, err := runBindingValidate(t, root)
	require.NoError(t, err)
}

// A folder no Flux Kustomization applies fails with the unbound diagnostic.
func TestValidateFluxBindingUnboundFolderIsNamed(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf("policy.yaml", "platform.yaml"),
		"clusters/prod/policy.yaml":        onlyFluxControllerPolicy,
		"clusters/prod/platform.yaml": fluxKustomization(
			"platform", "flux-system", "platform", "",
		),
		"platform/kustomization.yaml": kustomizationOf("configmap.yaml"),
		"platform/configmap.yaml":     configMap("platform-settings"),
		"stray/kustomization.yaml":    kustomizationOf("configmap.yaml"),
		"stray/configmap.yaml":        configMap("stray-settings"),
	})

	_, err := runBindingValidate(t, root)
	require.ErrorIs(t, err, workload.ErrUnboundFluxKustomization)
	require.ErrorContains(t, err, "ConfigMap/stray-settings")
	assert.NotContains(t, err.Error(), "platform-settings")
}

// Two Flux Kustomizations applying one folder as different identities fail
// with the ambiguous diagnostic.
func TestValidateFluxBindingAmbiguousIsNamed(t *testing.T) {
	t.Parallel()

	root := tenancyTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf(
			"policy.yaml", "platform.yaml", "tenant.yaml", "shadow.yaml",
		),
		"clusters/prod/shadow.yaml": fluxKustomization("shadow", "flux-system", "tenant", ""),
	})

	_, err := runBindingValidate(t, root)
	require.ErrorIs(t, err, workload.ErrAmbiguousFluxBinding)
	require.ErrorContains(t, err, "tenant-a/tenant")
	require.ErrorContains(t, err, "flux-system/shadow")
}

// A Flux Kustomization whose path leaves the validated tree fails with the
// out-of-tree diagnostic instead of being skipped.
func TestValidateFluxBindingOutsideTreeIsNamed(t *testing.T) {
	t.Parallel()

	root := tenancyTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf(
			"policy.yaml", "platform.yaml", "tenant.yaml", "escape.yaml",
		),
		"clusters/prod/escape.yaml": fluxKustomization("escape", "flux-system", "../elsewhere", ""),
	})

	_, err := runBindingValidate(t, root)
	require.ErrorIs(t, err, workload.ErrFluxBindingOutsideTree)
	require.ErrorContains(t, err, "flux-system/escape")
}

// A Flux Kustomization reading another source applies another tree, so it
// binds nothing here.
func TestValidateFluxBindingOtherSourceIsIgnored(t *testing.T) {
	t.Parallel()

	other := strings.Replace(
		fluxKustomization("tenant-repo", "tenant-a", ".", "  serviceAccountName: tenant-a\n"),
		"    name: flux-system\n    namespace: flux-system\n",
		"    name: tenant-repo\n", 1,
	)

	root := writeTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf(
			"policy.yaml", "platform.yaml", "tenant-repo.yaml",
		),
		"clusters/prod/policy.yaml": onlyFluxControllerPolicy,
		"clusters/prod/platform.yaml": fluxKustomization(
			"platform", "flux-system", "platform", "",
		),
		"clusters/prod/tenant-repo.yaml": other,
		"platform/kustomization.yaml":    kustomizationOf("configmap.yaml"),
		"platform/configmap.yaml":        configMap("platform-settings"),
	})

	_, err := runBindingValidate(t, root)
	require.NoError(t, err)
}

// tunedValuePolicy rejects a ConfigMap still carrying the chart's default value.
const tunedValuePolicy = `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: tuned-value
spec:
  validationFailureAction: Enforce
  rules:
    - name: not-default
      match:
        any:
          - resources:
              kinds:
                - ConfigMap
      validate:
        message: "the chart value must be tuned"
        pattern:
          data:
            key: "!default"
`

// With Helm rendering on, the Flux patch reaches the HelmRelease before the
// chart is rendered, so the rendered ConfigMap carries the patched value.
func TestValidateFluxBindingPatchAppliesBeforeHelmRender(t *testing.T) {
	t.Parallel()

	tree := func(extra string) string {
		root := writeTree(t, map[string]string{
			"clusters/prod/kustomization.yaml": kustomizationOf("policy.yaml", "apps.yaml"),
			"clusters/prod/policy.yaml":        tunedValuePolicy,
			"clusters/prod/apps.yaml": fluxKustomization(
				"apps", "flux-system", "apps/chart", extra,
			),
		})
		dir := filepath.Join(root, "apps", "chart")
		require.NoError(t, os.MkdirAll(dir, 0o750))
		writeKustomizationFiles(t, dir, localChartURL(t, "validchart"))

		return root
	}

	patch := `  patches:
    - target:
        kind: HelmRelease
        name: app
      patch: |
        - op: add
          path: /spec/values
          value: {"value": "tuned"}
`

	validate := func(root string) error {
		_, err := runValidate(t, root, "--kyverno-policies", "--skip-kinds", "OCIRepository",
			"--schema-location-mode", "only", "--schema-location", t.TempDir())

		return err
	}

	require.NoError(t, validate(tree(patch)), "the patched value reaches the rendered chart")

	err := validate(tree(""))
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation, "the default value still fails")
	require.ErrorContains(t, err, "ConfigMap/default/app-config")
}

// parentPatchTree has a parent Flux Kustomization applying a folder that holds a
// child Kustomization; extra is the parent's spec addition. The child names no
// serviceAccountName of its own and applies a ConfigMap.
func parentPatchTree(t *testing.T, extra string) string {
	t.Helper()

	return writeTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf("policy.yaml", "tenants.yaml"),
		"clusters/prod/policy.yaml":        onlyFluxControllerPolicy,
		"clusters/prod/tenants.yaml": fluxKustomization(
			"tenants", "flux-system", "tenants", extra,
		),
		"tenants/kustomization.yaml": kustomizationOf("tenant.yaml"),
		"tenants/tenant.yaml":        fluxKustomization("tenant", "tenant-a", "tenant", ""),
		"tenant/kustomization.yaml":  kustomizationOf("configmap.yaml"),
		"tenant/configmap.yaml":      configMap("tenant-settings"),
	})
}

// tenantIdentityPatch is a parent spec.patches entry that sets the child
// Kustomization's serviceAccountName.
const tenantIdentityPatch = `  patches:
    - target:
        kind: Kustomization
        group: kustomize.toolkit.fluxcd.io
      patch: |
        - op: add
          path: /spec/serviceAccountName
          value: tenant-a
`

// A child Kustomization is bound as its parent composes it: a parent patch that
// sets the child's serviceAccountName makes the child's folder apply as the
// tenant, so a platform-only rule rejects it.
func TestValidateFluxBindingChildReadFromComposedParent(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, parentPatchTree(t, tenantIdentityPatch))
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
	require.ErrorContains(t, err, "ConfigMap/tenant-settings")
	require.ErrorContains(t, err, "applied by Flux Kustomization tenant-a/tenant")
	require.ErrorContains(t, err, "as system:serviceaccount:tenant-a:tenant-a")
}

// Negative control: without the parent patch the child applies as Flux's own
// controller and the platform-only rule admits it.
func TestValidateFluxBindingChildWithoutParentPatchIsPlatform(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, parentPatchTree(t, ""))
	require.NoError(t, err)
}

// A folder with no kustomization file is evaluated as Flux generates it, not
// skipped: its manifests still face admission under the binding's identity.
func TestValidateFluxBindingGeneratedFolderIsEvaluated(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf("policy.yaml", "tenant.yaml"),
		"clusters/prod/policy.yaml":        onlyFluxControllerPolicy,
		"clusters/prod/tenant.yaml": fluxKustomization("tenant", "tenant-a", "tenant",
			"  serviceAccountName: tenant-a\n  targetNamespace: tenant-a\n"),
		"tenant/configmap.yaml": configMap("tenant-settings"),
	})

	_, err := runBindingValidate(t, root)
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
	require.ErrorContains(t, err, "ConfigMap/tenant-a/tenant-settings")
	require.ErrorContains(t, err, "as system:serviceaccount:tenant-a:tenant-a")
}

// A ClusterPolicy in a folder Flux generates a kustomization for is part of
// what the cluster admits, so it applies to every other bound folder.
func TestValidateFluxBindingGeneratedPolicyIsShared(t *testing.T) {
	t.Parallel()

	root := writeTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf("policies.yaml", "tenant.yaml"),
		"clusters/prod/policies.yaml": fluxKustomization(
			"policies", "flux-system", "policies", "",
		),
		"clusters/prod/tenant.yaml": fluxKustomization("tenant", "tenant-a", "tenant",
			"  serviceAccountName: tenant-a\n"),
		"policies/policy.yaml":      onlyFluxControllerPolicy,
		"tenant/kustomization.yaml": kustomizationOf("configmap.yaml"),
		"tenant/configmap.yaml":     configMap("tenant-settings"),
	})

	_, err := runBindingValidate(t, root)
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
	require.ErrorContains(t, err, `policy "platform-only-configmaps"`)
	require.ErrorContains(t, err, "ConfigMap/tenant-settings")
}

// forbiddenNamespaceRule rejects anything in the "forbidden" namespace.
const forbiddenNamespaceRule = `rules:
  - name: no-forbidden-namespace
    expression: '!has(object.metadata.namespace) || object.metadata.namespace != "forbidden"'
    message: "nothing may run in the forbidden namespace"
    severity: error
`

// The composed output of a bound folder faces the same CEL checks as the
// folder's own output: a targetNamespace that breaks a rule fails validation.
func TestValidateFluxBindingComposedOutputFacesCELRules(t *testing.T) {
	t.Parallel()

	tree := func(extra string) string {
		return writeTree(t, map[string]string{
			"clusters/prod/kustomization.yaml": kustomizationOf("apps.yaml"),
			"clusters/prod/apps.yaml": fluxKustomization(
				"apps",
				"flux-system",
				"apps",
				extra,
			),
			"apps/kustomization.yaml": kustomizationOf("configmap.yaml"),
			"apps/configmap.yaml":     configMap("app-settings"),
		})
	}

	validate := func(root string) error {
		_, err := runBindingValidate(t, root)
		if err != nil {
			return err
		}

		_, err = runValidate(t, root, "--kyverno-policies", "--skip-helm-render",
			"--rules", writeRulesFile(t, forbiddenNamespaceRule),
			"--schema-location-mode", "only", "--schema-location", t.TempDir())

		return err
	}

	err := validate(tree("  targetNamespace: forbidden\n"))
	require.ErrorContains(t, err, "no-forbidden-namespace")
	require.ErrorContains(t, err, "ConfigMap/forbidden/app-settings")

	require.NoError(
		t,
		validate(tree("  targetNamespace: allowed\n")),
		"the control namespace passes",
	)
}
