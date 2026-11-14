package workload_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noFloatingTagsPolicy is a CEL ValidatingPolicy refusing Pods whose containers
// use a floating `latest` tag, with the given validation action. Kyverno
// autogenerates the rule for pod controllers, so it also governs Deployments.
func noFloatingTagsPolicy(action string) string {
	return `apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: no-floating-tags
spec:
  validationActions: [` + action + `]
  matchConstraints:
    resourceRules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["pods"]
  validations:
    - expression: "object.spec.containers.all(c, !c.image.endsWith(':latest'))"
      message: "images must not use the floating latest tag"
`
}

// floatingDeployment is a Deployment in the apps namespace whose image is image.
func floatingDeployment(image string) string {
	return `apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: apps
spec:
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
    spec:
      containers:
        - name: web
          image: ` + image + `
`
}

// unlabelledConfigMap is a ConfigMap in the apps namespace without a team label.
const unlabelledConfigMap = `apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
  namespace: apps
data:
  key: value
`

// writeTwoLayerTree writes an infrastructure kustomization rendering
// infrastructure and an apps kustomization rendering apps, each a map of file
// name to content, and returns the tree's root.
func writeTwoLayerTree(t *testing.T, infrastructure, apps map[string]string) string {
	t.Helper()

	root := t.TempDir()

	for layer, files := range map[string]map[string]string{
		"infrastructure": infrastructure,
		"apps":           apps,
	} {
		dir := filepath.Join(root, layer)
		require.NoError(t, os.MkdirAll(dir, 0o750))

		resources := ""

		var resourcesSb83 strings.Builder
		for name, content := range files {
			resourcesSb83.WriteString("  - " + name + "\n")

			require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
		}

		resources += resourcesSb83.String()

		require.NoError(t, os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte(
			"apiVersion: kustomize.config.k8s.io/v1beta1\nkind: Kustomization\nresources:\n"+
				resources,
		), 0o600))
	}

	return root
}

// An enforcing ValidatingPolicy delivered by the infrastructure layer fails a
// violating Deployment in the apps layer (devantler-tech/ksail#8395).
func TestValidateKyvernoClusterValidatingPolicyFromAnotherKustomizationFails(t *testing.T) {
	t.Parallel()

	root := writeTwoLayerTree(t,
		map[string]string{"policy.yaml": noFloatingTagsPolicy("Deny")},
		map[string]string{"deployment.yaml": floatingDeployment("nginx:latest")},
	)

	_, err := runValidate(t, root, "--kyverno-policies")
	require.Error(t, err, "a policy from another kustomization must still be enforced")
	require.ErrorContains(t, err, `policy "no-floating-tags"`)
	require.ErrorContains(t, err, "Deployment/apps/web")
}

// A compliant workload in another kustomization passes the same policy.
func TestValidateKyvernoClusterValidatingPolicyFromAnotherKustomizationPasses(t *testing.T) {
	t.Parallel()

	root := writeTwoLayerTree(t,
		map[string]string{"policy.yaml": noFloatingTagsPolicy("Deny")},
		map[string]string{"deployment.yaml": floatingDeployment("nginx:1.27.0")},
	)

	_, err := runValidate(t, root, "--kyverno-policies")
	require.NoError(t, err)
}

// An audit-only ValidatingPolicy from another kustomization warns, never fails.
func TestValidateKyvernoClusterValidatingPolicyFromAnotherKustomizationAuditWarns(t *testing.T) {
	t.Parallel()

	root := writeTwoLayerTree(t,
		map[string]string{"policy.yaml": noFloatingTagsPolicy("Audit")},
		map[string]string{"deployment.yaml": floatingDeployment("nginx:latest")},
	)

	output, err := runValidate(t, root, "--kyverno-policies")
	require.NoError(t, err, "an audit-only failure must not fail validation")
	assert.Contains(t, output, `policy "no-floating-tags" failed (audit) for Deployment/apps/web`)
}

// A classic ClusterPolicy delivered by another kustomization is enforced too.
func TestValidateKyvernoClusterPolicyFromAnotherKustomizationFails(t *testing.T) {
	t.Parallel()

	root := writeTwoLayerTree(t,
		map[string]string{"policy.yaml": requireTeamLabelPolicy("Enforce")},
		map[string]string{"configmap.yaml": unlabelledConfigMap},
	)

	_, err := runValidate(t, root, "--kyverno-policies")
	require.Error(t, err)
	require.ErrorContains(t, err, `policy "require-team-label" rule "check-team" failed`)
	require.ErrorContains(t, err, "ConfigMap/apps/app-config")
}

// A kustomization's own copy of a cluster-scoped policy wins over a same-named
// one another kustomization renders, so the policy is applied once, as its own.
func TestValidateKyvernoOwnClusterPolicyWins(t *testing.T) {
	t.Parallel()

	root := writeTwoLayerTree(t,
		map[string]string{"policy.yaml": requireTeamLabelPolicy("Enforce")},
		map[string]string{
			"policy.yaml":    requireTeamLabelPolicy("Audit"),
			"configmap.yaml": unlabelledConfigMap,
		},
	)

	output, err := runValidate(t, root, "--kyverno-policies")
	require.NoError(t, err, "the apps layer's own audit-only copy must win")
	assert.Contains(t, output, `policy "require-team-label" rule "check-team" failed (audit)`)
}

// A namespaced Policy keeps its scoping: it applies only within the
// kustomization that renders it.
func TestValidateKyvernoNamespacedPolicyFromAnotherKustomizationIsNotApplied(t *testing.T) {
	t.Parallel()

	namespaced := `apiVersion: kyverno.io/v1
kind: Policy
metadata:
  name: require-team-label
  namespace: apps
spec:
  validationFailureAction: Enforce
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

	root := writeTwoLayerTree(t,
		map[string]string{"policy.yaml": namespaced},
		map[string]string{"configmap.yaml": unlabelledConfigMap},
	)

	_, err := runValidate(t, root, "--kyverno-policies")
	require.NoError(t, err, "a namespaced Policy must not reach another kustomization")
}
