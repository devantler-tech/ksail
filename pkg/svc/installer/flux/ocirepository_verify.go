package fluxinstaller

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v7/pkg/client/flux"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// newUnstructuredClient builds the client used to read the live OCIRepository. It is
// a package-level var, like loadRESTConfig, so tests can inject a fake and
// exercise the real read-and-compare path instead of stubbing its result.
//
//nolint:gochecknoglobals // Allows mocking for tests
var newUnstructuredClient = func(restConfig *rest.Config) (dynamic.Interface, error) {
	return dynamic.NewForConfig(restConfig)
}

// VerifyDrifted reports whether the live flux-system OCIRepository is missing
// the spec.verify block that spec.workload.flux.verify configures.
//
// This is the only signal a verify-only configuration change produces. The
// structural diff compares the previous cluster spec against the new one, so it
// sees a change the first time verify is configured and never again — while the
// block is applied to the cluster exclusively by SetupInstance, which cluster
// update reaches only through a handler some *other* field's change triggered.
// Configuring verify therefore left the live source unverified, with a green
// deploy and no signal at all (platform#2922).
//
// Flux only, and a no-op when verification is not configured: KSail asserts the
// configured block, and never removes or judges one it did not configure.
// Errors reading the cluster are returned to the caller, which reports them as
// warnings rather than blocking the update.
func VerifyDrifted(
	ctx context.Context,
	kubeconfig, kubeContext string,
	clusterCfg *v1alpha1.Cluster,
) (bool, error) {
	if clusterCfg == nil {
		return false, nil
	}

	if clusterCfg.Spec.Cluster.GitOpsEngine != v1alpha1.GitOpsEngineFlux {
		return false, nil
	}

	verify := clusterCfg.Spec.Workload.Flux.Verify
	if !verify.Enabled() {
		return false, nil
	}

	current, found, err := currentOCIRepositoryVerify(ctx, kubeconfig, kubeContext)
	if err != nil {
		return false, err
	}

	// No OCIRepository yet: the cluster has not been bootstrapped, so there is
	// nothing to re-assert against and SetupInstance will apply verify when it
	// creates the resource. Reporting drift here would emit a change on every
	// update against a cluster Flux does not manage.
	if !found {
		return false, nil
	}

	return !verifyMatches(current, buildVerifyPatch(verify)), nil
}

// currentOCIRepositoryVerify reads spec.verify off the live flux-system
// OCIRepository. found is false when the OCIRepository does not exist; a
// present resource carrying no spec.verify returns a nil map with found=true,
// which is the drift case this exists to catch.
func currentOCIRepositoryVerify(
	ctx context.Context,
	kubeconfig, kubeContext string,
) (map[string]any, bool, error) {
	restConfig, err := loadRESTConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, false, fmt.Errorf("build REST config: %w", err)
	}

	dynamicClient, err := newUnstructuredClient(restConfig)
	if err != nil {
		return nil, false, fmt.Errorf("create dynamic client: %w", err)
	}

	gvr := schema.GroupVersionResource{
		Group:    sourcev1.GroupVersion.Group,
		Version:  sourcev1.GroupVersion.Version,
		Resource: "ocirepositories",
	}

	repo, err := dynamicClient.Resource(gvr).
		Namespace(fluxclient.DefaultNamespace).
		Get(ctx, defaultOCIRepositoryName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("get OCIRepository: %w", err)
	}

	// A missing spec.verify is not an error — it is precisely the drift.
	current, _, err := unstructured.NestedMap(repo.Object, "spec", "verify")
	if err != nil {
		return nil, true, fmt.Errorf("read OCIRepository spec.verify: %w", err)
	}

	return current, true, nil
}
