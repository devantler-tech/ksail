package workload_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/require"
)

// releaseTeamLabelPolicy rejects a HelmRelease without a team label.
const releaseTeamLabelPolicy = `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-team
spec:
  validationFailureAction: Enforce
  rules:
    - name: team-label
      match:
        any:
          - resources:
              kinds:
                - HelmRelease
      validate:
        message: "HelmReleases need a team label"
        pattern:
          metadata:
            labels:
              team: "?*"
`

// commonMetadataTree applies the podinfo base through a Flux Kustomization
// with extra under its spec, against the team-label policy.
func commonMetadataTree(t *testing.T, extra string) string {
	t.Helper()

	return writeTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf("policy.yaml", "apps.yaml"),
		"clusters/prod/policy.yaml":        releaseTeamLabelPolicy,
		"clusters/prod/apps.yaml": fluxKustomization(
			"apps", "flux-system", "./apps/podinfo", extra,
		),
		"apps/podinfo/kustomization.yaml": kustomizationOf("helmrelease.yaml"),
		"apps/podinfo/helmrelease.yaml":   podinfoRelease,
	})
}

// kustomize-controller sets spec.commonMetadata on every applied object after
// the build, so a policy selecting on those labels sees them (devantler-tech/ksail#8561).
func TestValidateFluxBindingCommonMetadataIsApplied(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, commonMetadataTree(t,
		"  commonMetadata:\n    labels:\n      team: platform\n"))
	require.NoError(t, err, "the label commonMetadata sets meets the policy")
}

// Negative control: without commonMetadata the same HelmRelease fails.
func TestValidateFluxBindingWithoutCommonMetadataFails(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, commonMetadataTree(t, ""))
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
	require.ErrorContains(t, err, `policy "require-team" rule "team-label" failed`)
}

// A spec field validation does not reproduce fails the binding by name instead
// of being approximated: kubeConfig applies to another cluster.
func TestValidateFluxBindingUnmodelledFieldIsNamed(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, commonMetadataTree(t,
		"  kubeConfig:\n    secretRef:\n      name: remote\n"))
	require.ErrorIs(t, err, workload.ErrFluxFieldNotModelled)
	require.ErrorContains(t, err, "flux-system/apps sets spec.kubeConfig")
}

// nestedTree has a parent Flux Kustomization applying a folder that holds a
// child Kustomization with extra under its spec; the child applies a ConfigMap.
func nestedTree(t *testing.T, extra string) string {
	t.Helper()

	return writeTree(t, map[string]string{
		"clusters/prod/kustomization.yaml": kustomizationOf("policy.yaml", "tenants.yaml"),
		"clusters/prod/policy.yaml":        onlyFluxControllerPolicy,
		"clusters/prod/tenants.yaml": fluxKustomization(
			"tenants", "flux-system", "tenants", "",
		),
		"tenants/kustomization.yaml": kustomizationOf("tenant.yaml"),
		"tenants/tenant.yaml":        fluxKustomization("tenant", "tenant-a", "tenant", extra),
		"tenant/kustomization.yaml":  kustomizationOf("configmap.yaml"),
		"tenant/configmap.yaml":      configMap("tenant-settings"),
	})
}

// A child Kustomization rendered by a bound folder binds its own folder: with
// a serviceAccountName it applies as the tenant, so a platform-only rule
// rejects it.
func TestValidateFluxBindingNestedChildIsBound(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, nestedTree(t, "  serviceAccountName: tenant-a\n"))
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
	require.ErrorContains(t, err, "ConfigMap/tenant-settings")
	require.ErrorContains(t, err, "applied by Flux Kustomization tenant-a/tenant")
	require.ErrorContains(t, err, "as system:serviceaccount:tenant-a:tenant-a")
}

// Negative control: without a serviceAccountName the child applies as Flux's
// own controller and the platform-only rule admits it.
func TestValidateFluxBindingNestedChildWithoutAccountIsPlatform(t *testing.T) {
	t.Parallel()

	_, err := runBindingValidate(t, nestedTree(t, ""))
	require.NoError(t, err)
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

// A base policy an overlay strengthens reaches the cluster only as the overlay
// composes it, so the raw base never stands in for the strengthened policy that
// every other bound folder faces.
func TestValidateFluxBindingOverlayPolicyReplacesItsBase(t *testing.T) {
	t.Parallel()

	tree := func(overlayAction string) string {
		return writeTree(t, map[string]string{
			"clusters/prod/kustomization.yaml": kustomizationOf("policies.yaml", "tenant.yaml"),
			"clusters/prod/policies.yaml": fluxKustomization(
				"policies", "flux-system", "policies/prod", "",
			),
			"clusters/prod/tenant.yaml": fluxKustomization("tenant", "tenant-a", "tenant",
				"  serviceAccountName: tenant-a\n"),
			"policies/base/kustomization.yaml": kustomizationOf("policy.yaml"),
			"policies/base/policy.yaml": strings.Replace(
				onlyFluxControllerPolicy, "Enforce", "Audit", 1,
			),
			"policies/prod/kustomization.yaml": kustomizationOf("../base") +
				"patches:\n  - target:\n      kind: ClusterPolicy\n" +
				"    patch: |-\n      - op: replace\n        path: /spec/validationFailureAction\n" +
				"        value: " + overlayAction + "\n",
			"tenant/kustomization.yaml": kustomizationOf("configmap.yaml"),
			"tenant/configmap.yaml":     configMap("tenant-settings"),
		})
	}

	_, err := runBindingValidate(t, tree("Enforce"))
	require.ErrorIs(t, err, workload.ErrKyvernoPolicyViolation)
	require.ErrorContains(t, err, `policy "platform-only-configmaps"`)

	_, err = runBindingValidate(t, tree("Audit"))
	require.NoError(t, err, "control: the overlay keeps the base in Audit")
}
