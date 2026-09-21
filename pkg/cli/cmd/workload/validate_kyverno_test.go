package workload_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireTeamLabelPolicy is a ClusterPolicy requiring a `team` label on
// ConfigMaps, with the given validationFailureAction.
func requireTeamLabelPolicy(action string) string {
	return `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-team-label
spec:
  validationFailureAction: ` + action + `
  rules:
    - name: check-team
      match:
        any:
          - resources:
              kinds:
                - ConfigMap
      validate:
        message: "ConfigMaps must carry a team label"
        pattern:
          metadata:
            labels:
              team: "?*"
`
}

// writeKyvernoKustomization writes a kustomization whose output holds policy and
// a ConfigMap (labelled team=platform when labelled is true), and returns its
// directory. The policy ships in the same output as the resource it governs.
func writeKyvernoKustomization(t *testing.T, policy string, labelled bool) string {
	t.Helper()

	dir := t.TempDir()

	labels := ""
	if labelled {
		labels = "\n  labels:\n    team: platform"
	}

	files := map[string]string{
		"kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - policy.yaml
  - configmap.yaml
`,
		"policy.yaml": policy,
		"configmap.yaml": `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: default` + labels + `
data:
  key: value
`,
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	return dir
}

func TestValidateKyvernoPoliciesOffByDefault(t *testing.T) {
	t.Parallel()

	dir := writeKyvernoKustomization(t, requireTeamLabelPolicy("Enforce"), false)

	out, err := runValidate(t, dir)
	require.NoError(t, err, "without --kyverno-policies the policy must not be evaluated")
	assert.NotContains(t, out, "require-team-label")
}

func TestValidateKyvernoEnforceViolationFails(t *testing.T) {
	t.Parallel()

	dir := writeKyvernoKustomization(t, requireTeamLabelPolicy("Enforce"), false)

	_, err := runValidate(t, dir, "--kyverno-policies")
	require.Error(t, err, "an enforced policy failure must fail validation")
	require.ErrorContains(t, err, "kyverno policy violation")
	require.ErrorContains(t, err, `policy "require-team-label" rule "check-team" failed`)
	require.ErrorContains(t, err, "ConfigMap/default/app-config", "the failure names the resource")
	require.ErrorContains(t, err, "ConfigMaps must carry a team label", "the rule message surfaces")
}

func TestValidateKyvernoAuditViolationWarns(t *testing.T) {
	t.Parallel()

	dir := writeKyvernoKustomization(t, requireTeamLabelPolicy("Audit"), false)

	out, err := runValidate(t, dir, "--kyverno-policies")
	require.NoError(t, err, "an audit-only failure must not fail validation")
	assert.Contains(t, out, "Kyverno policy warning", "the audit failure is reported")
	assert.Contains(t, out, `rule "check-team" failed (audit)`)
}

func TestValidateKyvernoSatisfiedPolicyPasses(t *testing.T) {
	t.Parallel()

	dir := writeKyvernoKustomization(t, requireTeamLabelPolicy("Enforce"), true)

	out, err := runValidate(t, dir, "--kyverno-policies")
	require.NoError(t, err, "a document satisfying the policy passes")
	assert.NotContains(t, out, "Kyverno policy warning")
}

func TestValidateKyvernoSkippedKindIsNotEvaluated(t *testing.T) {
	t.Parallel()

	dir := writeKyvernoKustomization(t, requireTeamLabelPolicy("Enforce"), false)

	_, err := runValidate(t, dir, "--kyverno-policies", "--skip-kinds", "ConfigMap")
	require.NoError(t, err, "a kind excluded from validation cannot surface a policy failure")
}

func TestValidateKyvernoMalformedPolicyFails(t *testing.T) {
	t.Parallel()

	malformed := `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: broken
spec:
  rules: "not-a-list"
`
	dir := writeKyvernoKustomization(t, malformed, false)

	// Skipping ClusterPolicy keeps kubeconform from rejecting the schema first.
	// Policies are still collected under skip-kinds, so the load failure is ours.
	_, err := runValidate(t, dir, "--kyverno-policies", "--skip-kinds", "ClusterPolicy")
	require.Error(t, err, "a policy that cannot be loaded must not read as a clean run")
	require.ErrorContains(t, err, "load Kyverno policy")
}

// A Namespace is an object the cluster admits like any other, so a policy that
// matches Namespaces is evaluated against the rendered Namespace — the admission
// a real cluster would perform on create. Skipping the kind excludes it as a
// target while it stays available as namespace context.
func TestValidateKyvernoNamespaceIsEvaluatedUnlessSkipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	files := map[string]string{
		"kustomization.yaml": `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - policy.yaml
  - namespace.yaml
`,
		"policy.yaml": `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-pod-security-label
spec:
  validationFailureAction: Enforce
  rules:
    - name: check-enforce-label
      match:
        any:
          - resources:
              kinds:
                - Namespace
      validate:
        message: "Namespaces must set a pod-security enforce level"
        pattern:
          metadata:
            labels:
              pod-security.kubernetes.io/enforce: "?*"
`,
		"namespace.yaml": `apiVersion: v1
kind: Namespace
metadata:
  name: apps
`,
	}

	for name, content := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
	}

	_, err := runValidate(t, dir, "--kyverno-policies")
	require.Error(t, err, "an enforced Namespace policy failure must fail validation")
	require.ErrorContains(
		t,
		err,
		`policy "require-pod-security-label" rule "check-enforce-label" failed`,
	)
	require.ErrorContains(t, err, "Namespace/apps")

	_, err = runValidate(t, dir, "--kyverno-policies", "--skip-kinds", "Namespace")
	require.NoError(t, err, "a skipped Namespace cannot surface a policy failure")
}
