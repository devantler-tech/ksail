package fluxinstaller

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v7/pkg/client/flux"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/yaml"
)

const (
	defaultOCIRepositoryName = fluxclient.DefaultNamespace
	ociRepositoriesCRDName   = "ocirepositories.source.toolkit.fluxcd.io"
)

// ociRepositoryPatcher handles patching OCIRepository resources for local registries.
type ociRepositoryPatcher struct {
	restConfig *rest.Config
	apiWaiter  *apiWaiter
}

// newOCIRepositoryPatcher creates a new OCIRepository patcher.
func newOCIRepositoryPatcher(
	restConfig *rest.Config,
	timeout, interval time.Duration,
) *ociRepositoryPatcher {
	return &ociRepositoryPatcher{
		restConfig: restConfig,
		apiWaiter:  newAPIWaiter(restConfig, timeout, interval),
	}
}

// waitForAPI waits for the OCIRepository API to be available with stabilization delay.
func (p *ociRepositoryPatcher) waitForAPI(ctx context.Context) error {
	err := p.apiWaiter.waitForAPIReady(ctx, sourcev1.GroupVersion, ociRepositoriesCRDName)
	if err != nil {
		return err
	}

	// Brief stabilization delay for OCIRepository API as well
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled during OCIRepository API stabilization: %w", ctx.Err())
	case <-time.After(apiStabilizationDelay):
	}

	return nil
}

type patchResult int

const (
	patchSuccess patchResult = iota
	patchRetry
	patchFailed
)

// ensureInsecure patches the OCIRepository with insecure: true for local registries.
// Uses a dynamic Kubernetes client (via patchOCIRepository) to bypass REST mapper cache issues.
func (p *ociRepositoryPatcher) ensureInsecure(ctx context.Context) error {
	return p.patchOCIRepository(ctx, "insecure", func(obj map[string]any) (bool, error) {
		insecure, found, _ := unstructured.NestedBool(obj, "spec", "insecure")
		if found && insecure {
			return true, nil
		}

		err := unstructured.SetNestedField(obj, true, "spec", "insecure")
		if err != nil {
			return false, fmt.Errorf("failed to set insecure field: %w", err)
		}

		return false, nil
	})
}

// buildVerifyPatch renders the OCIRepository spec.verify block from KSail's
// workload verify config as an unstructured map suitable for SetNestedMap.
func buildVerifyPatch(cfg v1alpha1.FluxVerifySpec) map[string]any {
	verify := map[string]any{"provider": strings.TrimSpace(cfg.Provider)}

	if name := strings.TrimSpace(cfg.SecretRef.Name); name != "" {
		verify["secretRef"] = map[string]any{"name": name}
	}

	if len(cfg.MatchOIDCIdentity) > 0 {
		matchers := make([]any, 0, len(cfg.MatchOIDCIdentity))
		for _, identity := range cfg.MatchOIDCIdentity {
			matchers = append(matchers, map[string]any{
				"issuer":  identity.Issuer,
				"subject": identity.Subject,
			})
		}

		verify["matchOIDCIdentity"] = matchers
	}

	return verify
}

// applyVerify sets spec.verify on the OCIRepository object to the desired block.
// It reports done=true when the field already matches, so re-bootstrapping an
// already-verified OCIRepository performs no needless update.
func applyVerify(obj map[string]any, desired map[string]any) (bool, error) {
	current, found, err := unstructured.NestedMap(obj, "spec", "verify")
	if err == nil && found && reflect.DeepEqual(current, desired) {
		return true, nil
	}

	setErr := unstructured.SetNestedMap(obj, desired, "spec", "verify")
	if setErr != nil {
		return false, fmt.Errorf("failed to set verify field: %w", setErr)
	}

	return false, nil
}

// ensureVerify patches the flux-system OCIRepository with spec.verify so Flux
// verifies artifact signatures (cosign/notation) at pull time. It mirrors
// ensureInsecure: poll until the OCIRepository exists, then apply the verify
// block idempotently via a dynamic client (bypassing REST mapper cache issues).
func (p *ociRepositoryPatcher) ensureVerify(
	ctx context.Context,
	cfg v1alpha1.FluxVerifySpec,
) error {
	desired := buildVerifyPatch(cfg)

	return p.patchOCIRepository(ctx, "verify", func(obj map[string]any) (bool, error) {
		return applyVerify(obj, desired)
	})
}

// patchOCIRepository polls for the flux-system OCIRepository and applies a
// mutation to it, retrying on not-found/transient errors until the configured
// timeout. The apply callback returns done=true to stop without updating (e.g.
// the field already has the desired value).
func (p *ociRepositoryPatcher) patchOCIRepository(
	ctx context.Context,
	fieldName string,
	apply func(obj map[string]any) (done bool, err error),
) error {
	waitCtx, cancel := context.WithTimeout(ctx, p.apiWaiter.timeout)
	defer cancel()

	ticker := time.NewTicker(p.apiWaiter.interval)
	defer ticker.Stop()

	ociRepoGVR := schema.GroupVersionResource{
		Group:    sourcev1.GroupVersion.Group,
		Version:  sourcev1.GroupVersion.Version,
		Resource: "ocirepositories",
	}

	var lastErr error

	for {
		result, err := p.tryPatch(ctx, waitCtx, ociRepoGVR, apply)

		switch result {
		case patchSuccess:
			return nil
		case patchRetry:
			lastErr = err
		case patchFailed:
			return err
		}

		select {
		case <-waitCtx.Done():
			if lastErr == nil {
				lastErr = waitCtx.Err()
			}

			return fmt.Errorf(
				"timed out waiting for OCIRepository %s patch: %w",
				fieldName,
				lastErr,
			)
		case <-ticker.C:
			// Continue retry loop
		}
	}
}

// tryPatch fetches the OCIRepository once and applies the mutation callback.
// Returns whether to retry, succeed, or fail permanently.
func (p *ociRepositoryPatcher) tryPatch(
	ctx context.Context,
	waitCtx context.Context,
	gvr schema.GroupVersionResource,
	apply func(obj map[string]any) (done bool, err error),
) (patchResult, error) {
	dynamicClient, clientErr := dynamic.NewForConfig(p.restConfig)
	if clientErr != nil {
		return patchRetry, fmt.Errorf("failed to create dynamic client: %w", clientErr)
	}

	unstructuredRepo, err := dynamicClient.Resource(gvr).
		Namespace(fluxclient.DefaultNamespace).
		Get(waitCtx, defaultOCIRepositoryName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return patchRetry, errOCIRepositoryCreateTimout
		}

		return patchRetry, fmt.Errorf("failed to get OCIRepository: %w", err)
	}

	done, applyErr := apply(unstructuredRepo.Object)
	if applyErr != nil {
		return patchFailed, applyErr
	}

	if done {
		return patchSuccess, nil
	}

	_, updateErr := dynamicClient.Resource(gvr).
		Namespace(fluxclient.DefaultNamespace).
		Update(ctx, unstructuredRepo, metav1.UpdateOptions{})
	if updateErr == nil {
		return patchSuccess, nil
	}

	if isTransientAPIError(updateErr) {
		return patchRetry, fmt.Errorf("transient error updating OCIRepository: %w", updateErr)
	}

	return patchFailed, fmt.Errorf(
		"failed to update OCIRepository %s/%s: %w",
		fluxclient.DefaultNamespace, defaultOCIRepositoryName, updateErr,
	)
}

// buildSyncKustomize returns FluxInstance sync kustomize patches that must be
// applied by the Flux Operator before the generated sync resources can
// reconcile. Signature verification is seeded here instead of patched only
// after creation to avoid an initial unverified OCIRepository reconcile window.
func buildSyncKustomize(clusterCfg *v1alpha1.Cluster) (*SyncKustomize, error) {
	verify := clusterCfg.Spec.Workload.Flux.Verify
	if !verify.Enabled() {
		return nil, nil
	}

	verifyYAML, err := yaml.Marshal(buildVerifyPatch(verify))
	if err != nil {
		return nil, fmt.Errorf("marshal OCIRepository verify patch: %w", err)
	}

	return &SyncKustomize{Patches: []SyncKustomizePatch{{
		Target: SyncKustomizePatchTarget{
			Kind: fluxOCIRepositoryKind,
			Name: defaultOCIRepositoryName,
		},
		Patch: "- op: add\n  path: /spec/verify\n  value:\n" + indentYAML(string(verifyYAML), "    "),
	}}}, nil
}

func indentYAML(value string, indent string) string {
	lines := strings.Split(value, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + line
		}
	}

	return strings.Join(lines, "\n")
}
