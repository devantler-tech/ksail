package helm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/release"
	"helm.sh/helm/v4/pkg/release/common"
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
	// argoCDClusterSelector selects the Secrets Argo CD reads its cluster registrations from.
	argoCDClusterSelector = "argocd.argoproj.io/secret-type=cluster"
	// releaseManifestReadTimeout bounds reading the release record when the caller set no
	// deadline. Helm's storage driver takes no context, so the bound is set on its client.
	releaseManifestReadTimeout = time.Minute
)

var (
	errArgoCDReleaseNotOwned = errors.New("no Argo CD Application tracks the release")
	errReleaseRecordUnknown  = errors.New("unexpected Helm release record type")
	errReleaseRecordMissing  = errors.New("the release has no records")
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
	// RequiresPruning marks a live object Argo CD considers extraneous: it is not part of the
	// Application's desired state, so Argo CD does not own it.
	RequiresPruning bool `json:"requiresPruning"`
}

// argoCDReleaseOwnershipLabels recognizes an Argo CD Application that tracks any object of the
// release, whether or not the Helm record carries an ownership label: Argo CD renders charts
// itself and writes no Helm records, so an Application that adopted a release KSail installed
// leaves the record unlabelled. A cluster without the Argo CD API is not owned. Any other
// failure to read Applications or the release is an error, never permission to change it.
func (c *Client) argoCDReleaseOwnershipLabels(
	ctx context.Context, releaseName, namespace string,
) (map[string]string, error) {
	// The lookup runs on create and install paths whose context may carry no deadline, and the
	// ownership client sets no request timeout: bound the whole lookup so a stalled Argo CD API
	// cannot hang KSail.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc

		ctx, cancel = context.WithTimeout(ctx, releaseManifestReadTimeout)
		defer cancel()
	}

	config, client, err := c.ownershipClient("Argo CD")
	if err != nil {
		return nil, err
	}

	resource := client.Resource(schema.GroupVersionResource{
		Group: "argoproj.io", Version: "v1alpha1", Resource: "applications",
	})

	return scanArgoCDApplications(ctx, resource, namespace, argoCDLookups{
		readReleased: func() ([]trackedResource, error) {
			return c.releaseResources(ctx, config, releaseName, namespace)
		},
		localClusterNames: func() (map[string]bool, error) {
			return argoCDLocalClusterNames(ctx, config)
		},
	})
}

// argoCDLookups holds the reads an ownership scan makes only when an Application needs them.
type argoCDLookups struct {
	// readReleased returns the objects of the release.
	readReleased func() ([]trackedResource, error)
	// localClusterNames returns the names Argo CD cluster secrets register for this cluster.
	localClusterNames func() (map[string]bool, error)
}

// scanArgoCDApplications pages through every Application and reports the first that tracks an
// object of the release. The release's objects are read only once an Application exists.
func scanArgoCDApplications(
	ctx context.Context,
	resource dynamic.NamespaceableResourceInterface,
	namespace string,
	lookups argoCDLookups,
) (map[string]string, error) {
	options := metav1.ListOptions{Limit: argoCDOwnershipPageSize}
	isLocal := newLocalDestination(lookups.localClusterNames)

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
			released, err = lookups.readReleased()
			if err != nil {
				return nil, fmt.Errorf("read Argo CD release ownership: %w", err)
			}

			loaded = true
		}

		labels, ownershipErr := argoCDOwnershipLabels(list.Items, released, namespace, isLocal)
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
	items []unstructured.Unstructured,
	released []trackedResource,
	namespace string,
	isLocal func(argoCDApplication) (bool, error),
) (map[string]string, error) {
	for _, item := range items {
		var application argoCDApplication

		err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &application)
		if err != nil {
			return nil, fmt.Errorf("decode Argo CD release ownership: %w", err)
		}

		if !argoCDApplicationTracks(application, released, namespace) {
			continue
		}

		// An Application for another cluster tracks objects there, not here.
		local, err := isLocal(application)
		if err != nil {
			return nil, fmt.Errorf("read Argo CD release ownership: %w", err)
		}

		if local {
			return map[string]string{
				ArgoCDManagedByLabel: application.Metadata.Namespace + "/" + application.Metadata.Name,
			}, nil
		}
	}

	return nil, errArgoCDReleaseNotOwned
}

// newLocalDestination reports whether an Application deploys to the cluster Argo CD runs in.
// The in-cluster server and name need no lookup. Any other destination name may still be a
// cluster secret that registers the in-cluster server under another name, so names are resolved
// through the cluster secrets, read at most once.
func newLocalDestination(
	localClusterNames func() (map[string]bool, error),
) func(argoCDApplication) (bool, error) {
	var (
		names  map[string]bool
		loaded bool
	)

	return func(application argoCDApplication) (bool, error) {
		destination := application.Spec.Destination
		if destination.Server == argoCDInClusterServer || destination.Name == argoCDInClusterName {
			return true, nil
		}

		if destination.Server != "" || destination.Name == "" {
			return false, nil
		}

		if !loaded {
			var err error

			names, err = localClusterNames()
			if err != nil {
				return false, err
			}

			loaded = true
		}

		return names[destination.Name], nil
	}
}

// argoCDLocalClusterNames returns the names Argo CD cluster secrets give the in-cluster server.
// Argo CD reads cluster secrets from its own namespace, which need not be an Application's
// namespace, so every namespace is searched by the cluster-secret label.
func argoCDLocalClusterNames(ctx context.Context, config *rest.Config) (map[string]bool, error) {
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create client for Argo CD cluster secrets: %w", err)
	}

	names := map[string]bool{}
	options := metav1.ListOptions{
		LabelSelector: argoCDClusterSelector,
		Limit:         argoCDOwnershipPageSize,
	}

	for {
		secrets, err := clientset.CoreV1().Secrets(metav1.NamespaceAll).List(ctx, options)
		if err != nil {
			return nil, fmt.Errorf("list Argo CD cluster secrets: %w", err)
		}

		for _, secret := range secrets.Items {
			if string(secret.Data["server"]) == argoCDInClusterServer &&
				len(secret.Data["name"]) > 0 {
				names[string(secret.Data["name"])] = true
			}
		}

		options.Continue = secrets.Continue
		if options.Continue == "" {
			return names, nil
		}
	}
}

// argoCDApplicationTracks reports whether an Application tracks any of the release's objects.
func argoCDApplicationTracks(
	application argoCDApplication, released []trackedResource, namespace string,
) bool {
	for _, tracked := range application.Status.Resources {
		// Under label tracking, an object that merely carries the Application's instance label
		// is listed for pruning; that is not adoption.
		if tracked.RequiresPruning {
			continue
		}

		for _, object := range released {
			if sameObject(tracked, object, namespace) {
				return true
			}
		}
	}

	return false
}

// sameObject reports whether an object Argo CD tracks is one of the release's objects. Helm
// installs a namespaced object without a namespace into the release namespace. Argo CD lists a
// cluster-scoped object without a namespace, even when the chart template set one that the API
// server ignores, so such an entry matches on group, kind and name alone.
func sameObject(tracked, object trackedResource, releaseNamespace string) bool {
	if tracked.Group != object.Group || tracked.Kind != object.Kind || tracked.Name != object.Name {
		return false
	}

	return tracked.Namespace == "" ||
		tracked.Namespace == object.Namespace ||
		(object.Namespace == "" && tracked.Namespace == releaseNamespace)
}

// releaseResources returns the objects in the latest and the deployed record of the release,
// read from the release storage the way Helm does, within the caller's deadline.
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

	history, err := helmv4storage.Init(storageDriver).History(releaseName)
	if err != nil {
		return nil, fmt.Errorf("read the records of release %q: %w", releaseName, err)
	}

	records, err := latestAndDeployedRecords(history)
	if err != nil {
		return nil, fmt.Errorf("read the records of release %q: %w", releaseName, err)
	}

	var resources []trackedResource

	for _, record := range records {
		declared, err := manifestResources(record.Manifest)
		if err != nil {
			return nil, err
		}

		resources = append(resources, declared...)
	}

	return resources, nil
}

// latestAndDeployedRecords returns the latest record of a release and, when that record is not
// the deployed one (a failed or pending upgrade), the latest deployed record too: a failed
// upgrade's manifest can lack objects the deployed revision still runs.
func latestAndDeployedRecords(history []release.Releaser) ([]*v1.Release, error) {
	var latest, deployed *v1.Release

	for _, entry := range history {
		record, ok := entry.(*v1.Release)
		if !ok || record == nil {
			return nil, fmt.Errorf("%w: %T", errReleaseRecordUnknown, entry)
		}

		latest = newerRecord(latest, record)

		if record.Info != nil && record.Info.Status == common.StatusDeployed {
			deployed = newerRecord(deployed, record)
		}
	}

	if latest == nil {
		return nil, errReleaseRecordMissing
	}

	if deployed == nil || deployed == latest {
		return []*v1.Release{latest}, nil
	}

	return []*v1.Release{latest, deployed}, nil
}

// newerRecord returns whichever record has the higher revision; current may be nil.
func newerRecord(current, candidate *v1.Release) *v1.Release {
	if current == nil || candidate.Version > current.Version {
		return candidate
	}

	return current
}

// manifestResources lists the objects a rendered release manifest declares. Helm installs the
// items of a List document, and Argo CD tracks those items, so a List is flattened.
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

		resources, err = appendDeclared(resources, &object)
		if err != nil {
			return nil, fmt.Errorf("parse the release manifest: %w", err)
		}
	}
}

// appendDeclared appends an object, or each item of a List, to the declared objects.
func appendDeclared(
	resources []trackedResource, object *unstructured.Unstructured,
) ([]trackedResource, error) {
	if !object.IsList() {
		return append(resources, trackedResource{
			Group:     object.GroupVersionKind().Group,
			Kind:      object.GetKind(),
			Namespace: object.GetNamespace(),
			Name:      object.GetName(),
		}), nil
	}

	err := object.EachListItem(func(item runtime.Object) error {
		entry, ok := item.(*unstructured.Unstructured)
		if !ok || entry.GetKind() == "" {
			return nil
		}

		var err error

		resources, err = appendDeclared(resources, entry)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("read the items of a List document: %w", err)
	}

	return resources, nil
}
