package helm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	v1 "helm.sh/helm/v4/pkg/release/v1"
	helmv4storage "helm.sh/helm/v4/pkg/storage"
	helmv4driver "helm.sh/helm/v4/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ArgoCDManagedByLabel is the ownership key GetReleaseStorageLabels reports when an Argo CD
// Application tracks a release's resources. It is the key KSail's GitOps ownership check reads
// for Argo CD.
const ArgoCDManagedByLabel = "argocd.argoproj.io/managed-by"

const (
	argoCDOwnershipPageSize = 200
	// argoCDInClusterServer and argoCDInClusterName are the two ways an Application names the
	// cluster Argo CD itself runs in.
	argoCDInClusterServer = "https://kubernetes.default.svc"
	argoCDInClusterName   = "in-cluster"
	// releaseManifestReadTimeout bounds reading the release record when the caller set no
	// deadline. Helm's storage driver takes no context, so the bound is set on its client.
	releaseManifestReadTimeout = time.Minute
)

var (
	errArgoCDReleaseNotOwned = errors.New("no Argo CD Application tracks the release")
	errReleaseRecordUnknown  = errors.New("unexpected Helm release record type")
)

// argoCDApplication holds the fields of an Argo CD Application the ownership check reads.
type argoCDApplication struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Destination struct {
			Server string `json:"server"`
			Name   string `json:"name"`
		} `json:"destination"`
	} `json:"spec"`
	Status struct {
		Resources []trackedResource `json:"resources"`
	} `json:"status"`
}

// trackedResource identifies one Kubernetes object, as Argo CD lists the objects it tracks.
type trackedResource struct {
	Group     string `json:"group"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// argoCDReleaseOwnershipLabels recognizes an Argo CD Application that tracks any object of the
// release, whether or not the Helm record carries an ownership label: Argo CD renders charts
// itself and writes no Helm records, so an Application that adopted a release KSail installed
// leaves the record unlabelled. A cluster without the Argo CD API is not owned. Any other
// failure to read Applications or the release is an error, never permission to change it.
func (c *Client) argoCDReleaseOwnershipLabels(
	ctx context.Context, releaseName, namespace string,
) (map[string]string, error) {
	config, err := c.settings.RESTClientGetter().ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("get REST config for Argo CD release ownership: %w", err)
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create client for Argo CD release ownership: %w", err)
	}

	resource := client.Resource(schema.GroupVersionResource{
		Group: "argoproj.io", Version: "v1alpha1", Resource: "applications",
	})

	return scanArgoCDApplications(ctx, resource, namespace, func() ([]trackedResource, error) {
		return c.releaseResources(ctx, config, releaseName, namespace)
	})
}

// scanArgoCDApplications pages through every Application and reports the first that tracks an
// object of the release. The release's objects are read only once an Application exists.
func scanArgoCDApplications(
	ctx context.Context,
	resource dynamic.NamespaceableResourceInterface,
	namespace string,
	readReleased func() ([]trackedResource, error),
) (map[string]string, error) {
	options := metav1.ListOptions{Limit: argoCDOwnershipPageSize}

	var (
		released []trackedResource
		loaded   bool
		err      error
	)

	for {
		list, listErr := resource.List(ctx, options)
		if apierrors.IsNotFound(listErr) && options.Continue == "" {
			return nil, errArgoCDReleaseNotOwned
		}

		if listErr != nil {
			return nil, fmt.Errorf("read Argo CD release ownership: %w", listErr)
		}

		if !loaded && len(list.Items) > 0 {
			released, err = readReleased()
			if err != nil {
				return nil, fmt.Errorf("read Argo CD release ownership: %w", err)
			}

			loaded = true
		}

		labels, ownershipErr := argoCDOwnershipLabels(list.Items, released, namespace)
		if !errors.Is(ownershipErr, errArgoCDReleaseNotOwned) {
			return labels, ownershipErr
		}

		options.Continue = list.GetContinue()
		if options.Continue == "" {
			return nil, errArgoCDReleaseNotOwned
		}
	}
}

// argoCDOwnershipLabels checks one page of Applications without treating a malformed one as
// permission to change a release.
func argoCDOwnershipLabels(
	items []unstructured.Unstructured, released []trackedResource, namespace string,
) (map[string]string, error) {
	for _, item := range items {
		var application argoCDApplication

		err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &application)
		if err != nil {
			return nil, fmt.Errorf("decode Argo CD release ownership: %w", err)
		}

		if argoCDApplicationTracks(application, released, namespace) {
			return map[string]string{
				ArgoCDManagedByLabel: application.Metadata.Namespace + "/" + application.Metadata.Name,
			}, nil
		}
	}

	return nil, errArgoCDReleaseNotOwned
}

// argoCDApplicationTracks reports whether an Application deploying to this cluster tracks any
// of the release's objects. An Application for another cluster tracks objects there, not here.
func argoCDApplicationTracks(
	application argoCDApplication, released []trackedResource, namespace string,
) bool {
	destination := application.Spec.Destination
	if destination.Server != argoCDInClusterServer && destination.Name != argoCDInClusterName {
		return false
	}

	for _, tracked := range application.Status.Resources {
		for _, object := range released {
			if sameObject(tracked, object, namespace) {
				return true
			}
		}
	}

	return false
}

// sameObject reports whether an object Argo CD tracks is one of the release's objects. Helm
// installs a namespaced object without a namespace into the release namespace, and Argo CD
// lists a cluster-scoped object without one.
func sameObject(tracked, object trackedResource, releaseNamespace string) bool {
	if tracked.Group != object.Group || tracked.Kind != object.Kind || tracked.Name != object.Name {
		return false
	}

	return tracked.Namespace == object.Namespace ||
		(object.Namespace == "" && tracked.Namespace == releaseNamespace)
}

// releaseResources returns the objects in the latest record of the release, read from the
// release storage the way Helm does, within the caller's deadline.
func (c *Client) releaseResources(
	ctx context.Context, config *rest.Config, releaseName, namespace string,
) ([]trackedResource, error) {
	bounded := rest.CopyConfig(config)
	bounded.Timeout = releaseManifestReadTimeout

	if deadline, ok := ctx.Deadline(); ok {
		bounded.Timeout = max(time.Until(deadline), time.Millisecond)
	}

	clientset, err := kubernetes.NewForConfig(bounded)
	if err != nil {
		return nil, fmt.Errorf("create client for the release record: %w", err)
	}

	var storageDriver helmv4driver.Driver = helmv4driver.NewSecrets(
		clientset.CoreV1().Secrets(namespace),
	)
	if isConfigMapReleaseStorage(configuredReleaseStorageDriver()) {
		storageDriver = helmv4driver.NewConfigMaps(clientset.CoreV1().ConfigMaps(namespace))
	}

	last, err := helmv4storage.Init(storageDriver).Last(releaseName)
	if err != nil {
		return nil, fmt.Errorf("read the latest record of release %q: %w", releaseName, err)
	}

	record, ok := last.(*v1.Release)
	if !ok || record == nil {
		return nil, fmt.Errorf("%w: %T", errReleaseRecordUnknown, last)
	}

	return manifestResources(record.Manifest)
}

// manifestResources lists the objects a rendered release manifest declares.
func manifestResources(manifest string) ([]trackedResource, error) {
	decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), len(manifest)+1)

	var resources []trackedResource

	for {
		var object unstructured.Unstructured

		err := decoder.Decode(&object.Object)
		if errors.Is(err, io.EOF) {
			return resources, nil
		}

		if err != nil {
			return nil, fmt.Errorf("parse the release manifest: %w", err)
		}

		if len(object.Object) == 0 || object.GetKind() == "" {
			continue
		}

		resources = append(resources, trackedResource{
			Group:     object.GroupVersionKind().Group,
			Kind:      object.GetKind(),
			Namespace: object.GetNamespace(),
			Name:      object.GetName(),
		})
	}
}
