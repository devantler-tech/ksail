package fluxinstaller

import (
	"context"
	"fmt"
	"reflect"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v7/pkg/client/flux"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// VerifyDrifted reports whether the live flux-system OCIRepository's spec.verify
// differs from the block the resolved configuration would render.
//
// It exists because signature verification is applied by patching a resource the
// flux-operator generates, not by a field the structural diff walks. Without this
// comparison, configuring spec.workload.flux.verify produces no change at all, so
// cluster update runs no handler and the infrastructure source stays unverified —
// the config half correct and the enforcement half off (platform#2922).
//
// It reports false — never an error — when there is nothing to compare against:
// verification is not configured, or the OCIRepository does not exist yet. A
// missing resource is a bootstrap that has not reached the patch step, not drift.
//
// Verification being configured while the live resource carries no spec.verify is
// the opposite case and reports true. That is precisely the reported defect, and
// re-asserting the FluxInstance is the repair.
//
// kubeContext selects the cluster to read. It must be the same context the
// subsequent patch targets, or drift detected on one cluster would be repaired on
// another.
func VerifyDrifted(
	ctx context.Context,
	kubeconfig, kubeContext string,
	clusterCfg *v1alpha1.Cluster,
) (bool, error) {
	if clusterCfg == nil {
		return false, nil
	}

	cfg := clusterCfg.Spec.Workload.Flux.Verify
	if !cfg.Enabled() {
		return false, nil
	}

	current, found, err := currentOCIRepositoryVerify(ctx, kubeconfig, kubeContext)
	if err != nil {
		return false, err
	}

	return verifyDrift(current, found, cfg), nil
}

// verifyDrift decides the drift bit from the live spec.verify block and the
// configured one. Split out so the decision is testable without a cluster.
//
// repoFound=false means the OCIRepository does not exist yet: bootstrap has not
// reached the patch step, so there is nothing an update could repair.
func verifyDrift(
	current map[string]any,
	repoFound bool,
	cfg v1alpha1.FluxVerifySpec,
) bool {
	if !repoFound {
		return false
	}

	return !reflect.DeepEqual(current, buildVerifyPatch(cfg))
}

// currentOCIRepositoryVerify reads spec.verify from the live flux-system
// OCIRepository. found reports whether the OCIRepository itself exists; a resource
// that exists with no spec.verify returns (nil, true, nil), which is drift when
// verification is configured.
func currentOCIRepositoryVerify(
	ctx context.Context,
	kubeconfig, kubeContext string,
) (map[string]any, bool, error) {
	restConfig, err := loadRESTConfig(kubeconfig, kubeContext)
	if err != nil {
		return nil, false, fmt.Errorf("build REST config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
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
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}

		return nil, false, fmt.Errorf("get OCIRepository: %w", err)
	}

	verify, verifyFound, err := unstructured.NestedMap(repo.Object, "spec", "verify")
	if err != nil {
		// A spec.verify of an unexpected shape is not a value KSail can compare
		// against, so report it absent and let the patch overwrite it.
		return nil, true, nil //nolint:nilerr // malformed live value is drift, not a caller error
	}

	if !verifyFound {
		return nil, true, nil
	}

	return verify, true, nil
}
