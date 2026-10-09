package calicoinstaller

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// PrerequisiteKubeconfigForTest serves prerequisite lifecycle requests locally.
func PrerequisiteKubeconfigForTest(t *testing.T) string {
	t.Helper()

	fixture := newPrerequisiteFixture(t)
	fixture.seedOperatorHistory()

	return fixture.kubeconfig
}

// PrerequisiteManifestForTest is a complete CRD and admission policy fixture.
const PrerequisiteManifestForTest = prerequisiteFixtureManifest

// ObserveOperatorPrerequisiteForTest exercises recognition and subsequent content binding.
func ObserveOperatorPrerequisiteForTest(
	t *testing.T,
	desired, live *unstructured.Unstructured,
) (string, func(map[string]any) error, error) {
	t.Helper()
	fixture := newPrerequisiteFixture(t)
	seedOperatorDependency(t, fixture)
	metadata := &unstructured.Unstructured{Object: fixture.objects[fixtureDependencyPath]}
	policy := live.DeepCopy()
	policy.SetUID(metadata.GetUID())
	policy.SetResourceVersion(metadata.GetResourceVersion())
	policy.SetLabels(metadata.GetLabels())

	if policy.GetKind() == mutatingAdmissionPolicyKind {
		policy.SetLabels(map[string]string{
			"operator.tigera.io/mutating-admission-policy": "managed",
		})
	}

	policy.SetOwnerReferences(metadata.GetOwnerReferences())
	delete(fixture.objects, fixtureDependencyPath)

	resource, err := prerequisiteResource(policy)
	if err != nil {
		return "", nil, err
	}

	path := "/apis/" + resource.Group + "/" + resource.Version + "/" +
		resource.Resource + "/" + policy.GetName()
	fixture.objects[path] = policy.Object
	ctx := context.Background()

	plan, _, err := dependencyInstaller(fixture).loadPrerequisitePlan(ctx)
	if err != nil {
		return "", nil, err
	}

	err = plan.inspectDependency(ctx, desired, policy)
	if err != nil {
		return "", nil, err
	}

	revalidate := func(spec map[string]any) error {
		fixture.mu.Lock()
		fixture.objects[path]["spec"] = spec
		fixture.mu.Unlock()

		return plan.revalidateDependencies(ctx)
	}

	return plan.state.Dependencies[0].SpecSHA256, revalidate, nil
}

// PrerequisiteSpecDigestForTest exposes the unchanged observed-content fingerprint.
func PrerequisiteSpecDigestForTest(object *unstructured.Unstructured) (string, error) {
	return prerequisiteSpecDigest(object)
}

// SetAPIServerCheckerForTest overrides the API server stability checker for unit testing.
// This avoids needing a live Kubernetes cluster when testing the Install path.
func (c *Installer) SetAPIServerCheckerForTest(fn func(ctx context.Context) error) {
	c.apiServerChecker = fn
}

// SetRetryBackoffForTest overrides the retry backoff function for unit testing.
func (c *Installer) SetRetryBackoffForTest(fn func(ctx context.Context) error) {
	c.retryBackoff = fn
}
