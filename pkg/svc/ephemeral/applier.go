package ephemeral

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	admissionRequestTimeout = 30 * time.Second
	crdPollInterval         = 250 * time.Millisecond
)

// Applier submits workload resources to an explicitly selected cluster.
// Each kind is resolved through context-bound discovery, including newly installed CRDs.
type Applier struct {
	client    dynamic.Interface
	discovery rest.Interface
}

// NewApplier creates an admission client without falling back to the user's current context.
func NewApplier(kubeconfigPath, kubeContext string) (*Applier, error) {
	if kubeconfigPath == "" || kubeContext == "" {
		return nil, fmt.Errorf(
			"%w: ephemeral kubeconfig and context must both be explicit",
			ErrInput,
		)
	}

	config, err := k8s.BuildRESTConfig(kubeconfigPath, kubeContext)
	if err != nil {
		return nil, fmt.Errorf("configure ephemeral admission: %w", err)
	}

	config.Timeout = admissionRequestTimeout

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create ephemeral dynamic client: %w", err)
	}

	discoveryClient, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create ephemeral discovery client: %w", err)
	}

	return &Applier{client: client, discovery: discoveryClient.RESTClient()}, nil
}

// Apply submits one resource through strict server-side apply, without taking field ownership by force.
func (a *Applier) Apply(ctx context.Context, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()

	apiResource, err := a.resolve(ctx, gvk)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", resourceIdentity(obj), err)
	}

	gvr := gvk.GroupVersion().WithResource(apiResource.Name)
	applied := obj.DeepCopy()

	var resource dynamic.ResourceInterface

	if apiResource.Namespaced {
		namespace := applied.GetNamespace()
		if namespace == "" {
			namespace = metav1.NamespaceDefault
			applied.SetNamespace(namespace)
		}

		resource = a.client.Resource(gvr).Namespace(namespace)
	} else {
		resource = a.client.Resource(gvr)
	}

	data, err := json.Marshal(applied.Object)
	if err != nil {
		return fmt.Errorf("encode %s: %w", resourceIdentity(obj), err)
	}

	_, err = resource.Patch(ctx, applied.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: "ksail-ephemeral", FieldValidation: metav1.FieldValidationStrict,
	})
	if err != nil {
		return fmt.Errorf("admission rejected %s: %w", resourceIdentity(obj), err)
	}

	return nil
}

// WaitForCRD observes Established before dependent resources are submitted.
func (a *Applier) WaitForCRD(ctx context.Context, name string) error {
	resource := a.client.Resource(schema.GroupVersionResource{
		Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions",
	})

	err := wait.PollUntilContextCancel(
		ctx,
		crdPollInterval,
		true,
		func(ctx context.Context) (bool, error) {
			obj, err := resource.Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return false, fmt.Errorf("read CRD %s: %w", name, err)
			}

			conditions, _, err := unstructured.NestedSlice(obj.Object, "status", "conditions")
			if err != nil {
				return false, fmt.Errorf("read CRD conditions: %w", err)
			}

			for _, raw := range conditions {
				condition, ok := raw.(map[string]any)
				if ok && condition["type"] == "Established" && condition["status"] == "True" {
					return true, nil
				}
			}

			return false, nil
		},
	)
	if err != nil {
		return fmt.Errorf("wait for CRD %s establishment: %w", name, err)
	}

	return nil
}

func (a *Applier) resolve(
	ctx context.Context,
	gvk schema.GroupVersionKind,
) (metav1.APIResource, error) {
	err := ctx.Err()
	if err != nil {
		return metav1.APIResource{}, fmt.Errorf("discovery cancelled: %w", err)
	}

	if len(validation.IsDNS1123Label(gvk.Version)) > 0 ||
		(gvk.Group != "" && len(validation.IsDNS1123Subdomain(gvk.Group)) > 0) {
		return metav1.APIResource{}, fmt.Errorf(
			"%w: invalid apiVersion %q",
			ErrInput,
			gvk.GroupVersion(),
		)
	}

	path := "/api/" + gvk.Version
	if gvk.Group != "" {
		path = "/apis/" + gvk.Group + "/" + gvk.Version
	}

	var resources metav1.APIResourceList

	err = a.discovery.Get().AbsPath(path).Do(ctx).Into(&resources)
	if err != nil {
		return metav1.APIResource{}, fmt.Errorf("discover %s: %w", gvk.GroupVersion(), err)
	}

	for _, resource := range resources.APIResources {
		if resource.Kind == gvk.Kind && !strings.Contains(resource.Name, "/") {
			return resource, nil
		}
	}

	return metav1.APIResource{}, fmt.Errorf("%w: no API resource for %s", ErrInput, gvk)
}

func resourceIdentity(obj *unstructured.Unstructured) string {
	return obj.GetKind() + "/" + obj.GetNamespace() + "/" + obj.GetName()
}
