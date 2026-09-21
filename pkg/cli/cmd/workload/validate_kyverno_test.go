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

// teamNamespacePolicy enforces a `team` label on ConfigMaps, but only in
// Namespaces labelled tier=prod, so evaluating it needs the Namespace's labels.
const teamNamespacePolicy = `apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: require-team-label-in-prod
spec:
  validationFailureAction: Enforce
  rules:
    - name: check-team
      match:
        any:
          - resources:
              kinds:
                - ConfigMap
              namespaceSelector:
                matchLabels:
                  tier: prod
      validate:
        message: "ConfigMaps in prod Namespaces must carry a team label"
        pattern:
          metadata:
            labels:
              team: "?*"
`

// namespaceDoc renders a Namespace named apps with the given tier label.
func namespaceDoc(tier string) string {
	return "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: apps\n  labels:\n    tier: " + tier + "\n"
}

// writeLayeredTree writes a source tree with an `app` kustomization holding the
// policy and an unlabelled ConfigMap in Namespace apps, plus one kustomization per
// entry of namespaceLayers rendering Namespace apps with that tier. When
// ownTier is non-empty the app kustomization renders Namespace apps itself too.
func writeLayeredTree(t *testing.T, ownTier string, namespaceLayers ...string) string {
	t.Helper()

	root := t.TempDir()

	write := func(dir string, files map[string]string) {
		require.NoError(t, os.MkdirAll(dir, 0o750))

		for name, content := range files {
			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
		}
	}

	appResources := "  - policy.yaml\n  - configmap.yaml\n"
	appFiles := map[string]string{
		"policy.yaml": teamNamespacePolicy,
		"configmap.yaml": "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: app-config\n" +
			"  namespace: apps\ndata:\n  key: value\n",
	}

	if ownTier != "" {
		appResources += "  - namespace.yaml\n"
		appFiles["namespace.yaml"] = namespaceDoc(ownTier)
	}

	appFiles["kustomization.yaml"] = "apiVersion: kustomize.config.k8s.io/v1beta1\n" +
		"kind: Kustomization\nresources:\n" + appResources
	write(filepath.Join(root, "app"), appFiles)

	for index, tier := range namespaceLayers {
		write(filepath.Join(root, "namespaces-"+string(rune('a'+index))), map[string]string{
			"kustomization.yaml": "apiVersion: kustomize.config.k8s.io/v1beta1\n" +
				"kind: Kustomization\nresources:\n  - namespace.yaml\n",
			"namespace.yaml": namespaceDoc(tier),
		})
	}

	return root
}

// A Namespace rendered by another kustomization supplies the labels a
// namespaceSelector needs, so the rule is evaluated and its enforced failure fails
// validation instead of being reported as not evaluable offline.
func TestValidateKyvernoUsesNamespaceFromAnotherKustomization(t *testing.T) {
	t.Parallel()

	_, err := runValidate(t, writeLayeredTree(t, "", "prod"), "--kyverno-policies")
	require.Error(t, err, "a Namespace from another kustomization must make the rule evaluable")
	require.ErrorContains(t, err, `policy "require-team-label-in-prod" rule "check-team" failed`)
	require.ErrorContains(t, err, "ConfigMap/apps/app-config")

	output, err := runValidate(t, writeLayeredTree(t, "", "dev"), "--kyverno-policies")
	require.NoError(t, err, "a Namespace outside the selector must not fail validation")
	assert.NotContains(t, output, "not evaluable offline")
}

// Two kustomizations rendering the same Namespace with different labels leave its
// labels unknown: the rule is reported as not evaluable, never guessed either way.
func TestValidateKyvernoConflictingNamespaceLabelsAreNotEvaluable(t *testing.T) {
	t.Parallel()

	output, err := runValidate(t, writeLayeredTree(t, "", "prod", "dev"), "--kyverno-policies")
	require.NoError(t, err, "conflicting Namespace labels must not produce an enforced failure")
	assert.Contains(
		t,
		output,
		`policy "require-team-label-in-prod" rule "check-team" not evaluable offline`,
	)
}

// A kustomization's own Namespace wins over one rendered elsewhere, even when the
// others disagree among themselves.
func TestValidateKyvernoOwnNamespaceWins(t *testing.T) {
	t.Parallel()

	_, err := runValidate(t, writeLayeredTree(t, "prod", "dev", "staging"), "--kyverno-policies")
	require.Error(t, err, "the kustomization's own prod Namespace must be used")
	require.ErrorContains(t, err, `policy "require-team-label-in-prod" rule "check-team" failed`)

	_, err = runValidate(t, writeLayeredTree(t, "dev", "prod"), "--kyverno-policies")
	require.NoError(
		t,
		err,
		"the kustomization's own dev Namespace must win over another's prod one",
	)
}

// A kustomization that renders a valid Namespace but fails validation on another
// document still lends that Namespace to the others, so their rules stay evaluable
// rather than being reported as not evaluable offline.
func TestValidateKyvernoUsesNamespaceFromAFailedKustomization(t *testing.T) {
	t.Parallel()

	root := writeLayeredTree(t, "", "prod")
	layer := filepath.Join(root, "namespaces-a")
	require.NoError(t, os.WriteFile(filepath.Join(layer, "kustomization.yaml"), []byte(
		"apiVersion: kustomize.config.k8s.io/v1beta1\n"+
			"kind: Kustomization\nresources:\n  - namespace.yaml\n  - invalid.yaml\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(layer, "invalid.yaml"), []byte(
		"apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: invalid\n  namespace: apps\n"+
			"data: not-a-map\n"), 0o600))

	output, err := runValidate(t, root, "--kyverno-policies")
	require.Error(t, err, "the invalid document must still fail validation")
	require.ErrorContains(t, err, `policy "require-team-label-in-prod" rule "check-team" failed`)
	assert.NotContains(t, output, "not evaluable offline")
}
