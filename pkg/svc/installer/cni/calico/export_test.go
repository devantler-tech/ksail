package calicoinstaller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// DependencyMigrationForTest exercises the prerequisite lifecycle against a local API fixture.
type DependencyMigrationForTest struct {
	fixture *prerequisiteFixture
	plan    *prerequisitePlan
}

// CalicoChartVersionForTest exposes the embedded chart version for lifecycle assertions.
func CalicoChartVersionForTest() string {
	return chartVersion()
}

// NewDependencyMigrationForTest records the installed chart's dependency content.
func NewDependencyMigrationForTest(t *testing.T, version string) *DependencyMigrationForTest {
	t.Helper()
	fixture := newPrerequisiteFixture(t)
	seedOperatorDependency(t, fixture)
	object := &unstructured.Unstructured{Object: fixture.objects[fixtureDependencyPath]}

	resource, err := prerequisiteResource(object)
	if err != nil {
		t.Fatal(err)
	}

	digest, err := prerequisiteSpecDigest(object)
	if err != nil {
		t.Fatal(err)
	}

	fixture.setInventory(t, prerequisiteState{
		Version: version, Complete: true, Resources: nil,
		Dependencies: []prerequisiteDependency{{
			prerequisiteRef: refForObject(resource, object),
			InstallationUID: "installation-uid", SpecSHA256: digest,
		}},
	})

	return &DependencyMigrationForTest{fixture: fixture, plan: nil}
}

// Plan renders the next chart's policy and observes the currently installed dependency.
func (f *DependencyMigrationForTest) Plan(spec map[string]any) error {
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("encode migration fixture policy: %w", err)
	}

	manifest := strings.Replace(
		prerequisiteFixtureManifest,
		"{failurePolicy: Fail}",
		string(data),
		1,
	)

	return f.PlanManifest(manifest)
}

// PlanManifest observes the inventory against a complete rendered chart fixture.
func (f *DependencyMigrationForTest) PlanManifest(manifest string) error {
	var err error

	f.plan, err = dependencyInstaller(f.fixture).planPrerequisites(context.Background(), manifest)

	return err
}

// Apply installs the owned prerequisites before the operator upgrade.
func (f *DependencyMigrationForTest) Apply() error {
	return f.plan.apply(context.Background())
}

// Complete validates the operator's resulting dependencies and saves the completed inventory.
func (f *DependencyMigrationForTest) Complete() error {
	return f.plan.complete(context.Background())
}

// Inventory returns the persisted chart version, completion flag and dependency digest.
func (f *DependencyMigrationForTest) Inventory() (string, bool, string, error) {
	_, state, err := readInstallerInventory(context.Background(), dependencyInstaller(f.fixture))
	if err != nil {
		return "", false, "", err
	}

	return state.Version, state.Complete, state.Dependencies[0].SpecSHA256, nil
}

// Dependency returns a copy of the operator-owned policy for simulating an operator update.
func (f *DependencyMigrationForTest) Dependency() *unstructured.Unstructured {
	f.fixture.mu.Lock()
	defer f.fixture.mu.Unlock()

	return (&unstructured.Unstructured{Object: f.fixture.objects[fixtureDependencyPath]}).DeepCopy()
}

// SetDependency simulates an API-observed change without using the installer's mutation path.
func (f *DependencyMigrationForTest) SetDependency(object *unstructured.Unstructured) {
	f.fixture.mu.Lock()
	defer f.fixture.mu.Unlock()

	f.fixture.objects[fixtureDependencyPath] = object.DeepCopy().Object
}

// Writes returns the API mutations made by the installer.
func (f *DependencyMigrationForTest) Writes() []string {
	f.fixture.mu.Lock()
	defer f.fixture.mu.Unlock()

	return append([]string(nil), f.fixture.writes...)
}

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
