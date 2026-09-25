package workload_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeRepeatedNamespacePolicies(t *testing.T, policy string) string {
	t.Helper()

	root := t.TempDir()
	for _, layer := range []string{"a", "b", "c"} {
		dir := filepath.Join(root, layer)
		require.NoError(t, os.MkdirAll(dir, 0o750))

		var resources strings.Builder
		resources.WriteString(policy)

		for _, namespace := range []string{"kube-system", "flux-system"} {
			for _, name := range []string{"first", "second"} {
				resources.WriteString(
					"\n---\napiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: " +
						name + "\n  namespace: " + namespace + "\ndata:\n  key: value\n",
				)
			}
		}

		require.NoError(
			t,
			os.WriteFile(filepath.Join(dir, "resources.yaml"), []byte(resources.String()), 0o600),
		)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(
			"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\n"+
				"resources: [resources.yaml]\n"), 0o600))
	}

	return root
}

func TestValidateKyvernoGroupsUnknownNamespaceWarnings(t *testing.T) {
	t.Parallel()

	for name, policy := range map[string]string{
		"classic": teamNamespacePolicy,
		"CEL": strings.Replace(requireTeamLabelValidatingPolicy("Deny"), "  matchConstraints:",
			"  matchConstraints:\n    namespaceSelector:\n      matchLabels:\n        tier: prod", 1),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := writeRepeatedNamespacePolicies(t, policy)
			output, err := runValidate(t, root, "--kyverno-policies")
			require.NoError(t, err)
			assert.Equal(t, 2, strings.Count(output, "Kyverno policy warning:"))
			assert.Equal(t, 2, strings.Count(output, "6 evaluations, 2 resources, 3 sources"))
			assert.Contains(t, output, `namespace "kube-system"`)
			assert.Contains(t, output, `namespace "flux-system"`)
			assert.Contains(t, output, "ConfigMap/kube-system/first")
			assert.Contains(t, output, filepath.Join(root, "a"))
		})
	}
}

func TestValidateKyvernoGroupingKeepsAuditFailures(t *testing.T) {
	t.Parallel()

	root := writeRepeatedNamespacePolicies(t, requireTeamLabelPolicy("Audit"))
	output, err := runValidate(t, root, "--kyverno-policies")
	require.NoError(t, err)
	assert.Equal(t, 12, strings.Count(output, "failed (audit)"))
	assert.NotContains(t, output, "evaluations,")
}

func TestValidateKyvernoGroupingKeepsDistinctPoliciesAndRules(t *testing.T) {
	t.Parallel()

	_, rule, found := strings.Cut(teamNamespacePolicy, "  rules:\n")
	require.True(t, found)

	twoRules := teamNamespacePolicy + strings.Replace(rule, "check-team", "check-owner", 1)
	twoPolicies := twoRules + "\n---\n" + strings.Replace(
		twoRules, "require-team-label-in-prod", "another-policy", 1,
	)
	output, err := runValidate(
		t,
		writeRepeatedNamespacePolicies(t, twoPolicies),
		"--kyverno-policies",
	)
	require.NoError(t, err)
	assert.Equal(t, 8, strings.Count(output, "Kyverno policy warning:"))
	assert.Equal(t, 8, strings.Count(output, "6 evaluations, 2 resources, 3 sources"))
}

func TestValidateKyvernoGroupingKeepsBlockingFailures(t *testing.T) {
	t.Parallel()

	policies := teamNamespacePolicy + "\n---\n" + requireTeamLabelPolicy("Enforce")
	output, err := runValidate(t, writeRepeatedNamespacePolicies(t, policies), "--kyverno-policies")
	require.Error(t, err)
	require.ErrorContains(t, err, "kyverno policy violation")
	assert.Equal(t, 12, strings.Count(err.Error(), `rule "check-team" failed for ConfigMap/`))
	assert.Equal(t, 2, strings.Count(output, "Kyverno policy warning:"))
}
