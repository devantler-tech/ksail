package flux

import (
	"context"
	"errors"
	"fmt"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

var (
	// ErrTypeMismatch is returned when type assertion fails in copySpec.
	ErrTypeMismatch = errors.New("type mismatch in copySpec")
	// ErrUnsupportedResourceType is returned when an unsupported resource type is passed to copySpec.
	ErrUnsupportedResourceType = errors.New("unsupported resource type")
)

const (
	// DefaultNamespace is the default namespace for Flux resources.
	DefaultNamespace = "flux-system"
	// SplitParts is the number of parts when splitting strings like "namespace/name" or "name.namespace".
	SplitParts = 2
)

// Client wraps flux API functionality.
type Client struct {
	ioStreams      genericiooptions.IOStreams
	kubeconfigPath string
	client         client.Client
}

// NewClient creates a new flux client instance.
func NewClient(ioStreams genericiooptions.IOStreams, kubeconfigPath string) *Client {
	return &Client{
		ioStreams:      ioStreams,
		kubeconfigPath: kubeconfigPath,
	}
}

// CreateCreateCommand returns the flux create command tree.
func (c *Client) CreateCreateCommand(kubeconfigPath string) *cobra.Command {
	c.kubeconfigPath = kubeconfigPath

	createCmd := &cobra.Command{
		Use:   "flux-create",
		Short: "Create Flux resources",
		Long:  "Create or update Flux sources and resources using Kubernetes APIs.",
	}

	// Add namespace flag to all commands
	createCmd.PersistentFlags().StringP(
		"namespace",
		"n",
		DefaultNamespace,
		"the namespace scope for this operation",
	)

	// Add sub-commands for flux create
	createCmd.AddCommand(c.createSourceCommand())
	createCmd.AddCommand(c.newCreateKustomizationCmd())
	createCmd.AddCommand(c.newCreateHelmReleaseCmd())

	return createCmd
}

// createSourceCommand creates the flux create source command.
func (c *Client) createSourceCommand() *cobra.Command {
	sourceCmd := &cobra.Command{
		Use:   "source",
		Short: "Create or update Flux sources",
	}

	// Add source sub-commands
	sourceCmd.AddCommand(c.newCreateSourceGitCmd())
	sourceCmd.AddCommand(c.newCreateSourceHelmCmd())
	sourceCmd.AddCommand(c.newCreateSourceOCICmd())

	return sourceCmd
}

// extractNameAndNamespace extracts the resource name and namespace from cobra command arguments.
// Returns name and namespace, with namespace defaulting to DefaultNamespace if not specified.
func extractNameAndNamespace(cmd *cobra.Command, args []string) (string, string) {
	name := args[0]

	namespace := cmd.Flag("namespace").Value.String()
	if namespace == "" {
		namespace = DefaultNamespace
	}

	return name, namespace
}

// Client configuration and initialization.

// getClient returns a controller-runtime client configured for Flux APIs.
func (c *Client) getClient() (client.Client, error) {
	if c.client != nil {
		return c.client, nil
	}

	config, err := c.getRestConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get REST config: %w", err)
	}

	scheme := runtime.NewScheme()

	err = sourcev1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add source-controller scheme: %w", err)
	}

	err = kustomizev1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add kustomize-controller scheme: %w", err)
	}

	err = helmv2.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add helm-controller scheme: %w", err)
	}

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}

	c.client = k8sClient

	return k8sClient, nil
}

// getRestConfig returns a REST config for the Kubernetes cluster.
func (c *Client) getRestConfig() (*rest.Config, error) {
	if c.kubeconfigPath != "" {
		cfg, err := clientcmd.BuildConfigFromFlags("", c.kubeconfigPath)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig path: %w", err)
		}

		return cfg, nil
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		configOverrides,
	)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	return config, nil
}

// Resource management helpers.

// exportResource exports a resource as YAML to stdout.
func (c *Client) exportResource(obj runtime.Object) error {
	data, err := yaml.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal resource: %w", err)
	}

	_, err = c.ioStreams.Out.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write output: %w", err)
	}

	return nil
}

// upsertResource creates or updates a Flux resource using the Kubernetes API.
// It takes a context, the resource object to create/update, the resource name,
// namespace, and a resourceKind string for logging purposes.
// The obj parameter must be a pointer to a Flux resource (e.g., *sourcev1.GitRepository).
// The existing parameter must be a pointer to the same type for fetching existing resources.
func (c *Client) upsertResource(
	ctx context.Context,
	obj client.Object,
	existing client.Object,
	resourceKind string,
) error {
	k8sClient, err := c.getClient()
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// Try to create the resource
	err = k8sClient.Create(ctx, obj)
	if err == nil {
		return c.printSuccess(obj, resourceKind, "created")
	}

	// If resource doesn't exist, return the error
	if client.IgnoreAlreadyExists(err) != nil {
		return fmt.Errorf("failed to create %s: %w", resourceKind, err)
	}

	// Resource exists, update it
	return c.updateExisting(ctx, k8sClient, obj, existing, resourceKind)
}

// printSuccess formats and prints a success message for resource operations.
func (c *Client) printSuccess(obj client.Object, resourceKind, action string) error {
	_, err := fmt.Fprintf(
		c.ioStreams.Out,
		"✓ %s %s/%s %s\n",
		resourceKind,
		obj.GetNamespace(),
		obj.GetName(),
		action,
	)
	if err != nil {
		return fmt.Errorf("failed to print success message: %w", err)
	}

	return nil
}

// updateExisting updates an existing Flux resource with new spec from obj.
func (c *Client) updateExisting(
	ctx context.Context,
	k8sClient client.Client,
	obj, existing client.Object,
	resourceKind string,
) error {
	err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}, existing)
	if err != nil {
		return fmt.Errorf("failed to get existing %s: %w", resourceKind, err)
	}

	err = copySpec(obj, existing)
	if err != nil {
		return fmt.Errorf("failed to copy spec: %w", err)
	}

	err = k8sClient.Update(ctx, existing)
	if err != nil {
		return fmt.Errorf("failed to update %s: %w", resourceKind, err)
	}

	return c.printSuccess(obj, resourceKind, "updated")
}

// Spec copying helpers.

// copySpec copies the Spec field from src to dst using type assertions.
func copySpec(src, dst client.Object) error {
	switch sourceObj := src.(type) {
	case *sourcev1.GitRepository:
		return copyGitRepositorySpec(sourceObj, dst)
	case *sourcev1.HelmRepository:
		return copyHelmRepositorySpec(sourceObj, dst)
	case *sourcev1.OCIRepository:
		return copyOCIRepositorySpec(sourceObj, dst)
	case *kustomizev1.Kustomization:
		return copyKustomizationSpec(sourceObj, dst)
	case *helmv2.HelmRelease:
		return copyHelmReleaseSpec(sourceObj, dst)
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedResourceType, src)
	}
}

// copyGitRepositorySpec copies the spec from a GitRepository source to destination.
func copyGitRepositorySpec(src *sourcev1.GitRepository, dst client.Object) error {
	dstObj, ok := dst.(*sourcev1.GitRepository)
	if !ok {
		return fmt.Errorf("%w: expected *sourcev1.GitRepository, got %T", ErrTypeMismatch, dst)
	}

	dstObj.Spec = src.Spec

	return nil
}

// copyHelmRepositorySpec copies the spec from a HelmRepository source to destination.
func copyHelmRepositorySpec(src *sourcev1.HelmRepository, dst client.Object) error {
	dstObj, ok := dst.(*sourcev1.HelmRepository)
	if !ok {
		return fmt.Errorf("%w: expected *sourcev1.HelmRepository, got %T", ErrTypeMismatch, dst)
	}

	dstObj.Spec = src.Spec

	return nil
}

// copyOCIRepositorySpec copies the spec from an OCIRepository source to destination.
func copyOCIRepositorySpec(src *sourcev1.OCIRepository, dst client.Object) error {
	dstObj, ok := dst.(*sourcev1.OCIRepository)
	if !ok {
		return fmt.Errorf("%w: expected *sourcev1.OCIRepository, got %T", ErrTypeMismatch, dst)
	}

	dstObj.Spec = src.Spec

	return nil
}

// copyKustomizationSpec copies the spec from a Kustomization source to destination.
func copyKustomizationSpec(src *kustomizev1.Kustomization, dst client.Object) error {
	dstObj, ok := dst.(*kustomizev1.Kustomization)
	if !ok {
		return fmt.Errorf("%w: expected *kustomizev1.Kustomization, got %T", ErrTypeMismatch, dst)
	}

	dstObj.Spec = src.Spec

	return nil
}

// copyHelmReleaseSpec copies the spec from a HelmRelease source to destination.
func copyHelmReleaseSpec(src *helmv2.HelmRelease, dst client.Object) error {
	dstObj, ok := dst.(*helmv2.HelmRelease)
	if !ok {
		return fmt.Errorf("%w: expected *helmv2.HelmRelease, got %T", ErrTypeMismatch, dst)
	}

	dstObj.Spec = src.Spec

	return nil
}
