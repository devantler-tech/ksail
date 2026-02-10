package localpathstorageinstaller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ErrManifestFetchFailed is returned when the manifest cannot be fetched from the remote URL.
var ErrManifestFetchFailed = errors.New("failed to fetch manifest")

const (
	// localPathProvisionerVersion is the version of Rancher local-path-provisioner to install.
	localPathProvisionerVersion = "v0.0.32"
	// localPathProvisionerManifestURL is the URL to the local-path-provisioner manifest.
	localPathProvisionerManifestURL = "https://raw.githubusercontent.com/rancher/local-path-provisioner/" +
		localPathProvisionerVersion + "/deploy/local-path-storage.yaml"
)

// Installer installs local-path-provisioner on Kind and Talos clusters.
type Installer struct {
	kubeconfig   string
	context      string
	timeout      time.Duration
	distribution v1alpha1.Distribution
}

// NewInstaller creates a new local-path-storage installer instance.
func NewInstaller(
	kubeconfig, context string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
) *Installer {
	return &Installer{
		kubeconfig:   kubeconfig,
		context:      context,
		timeout:      timeout,
		distribution: distribution,
	}
}

// Install installs local-path-provisioner if needed based on the distribution.
func (l *Installer) Install(ctx context.Context) error {
	// K3d already has local-path-provisioner by default, no action needed
	if l.distribution.ProvidesStorageByDefault() {
		return nil
	}

	// For Kind and Talos, install local-path-provisioner
	if l.distribution == v1alpha1.DistributionVanilla ||
		l.distribution == v1alpha1.DistributionTalos {
		return l.installLocalPathProvisioner(ctx)
	}

	return nil
}

// Uninstall is a no-op as we don't support uninstalling storage provisioners.
func (l *Installer) Uninstall(_ context.Context) error {
	return nil
}

// Images returns the container images used by local-path-provisioner.
// It fetches and parses the manifest from the upstream URL.
func (l *Installer) Images(ctx context.Context) ([]string, error) {
	// K3d/K3s already has local-path-provisioner, return empty list
	if l.distribution.ProvidesStorageByDefault() {
		return nil, nil
	}

	// Fetch the manifest from the URL
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		localPathProvisionerManifestURL,
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Use a client with timeout to avoid hanging indefinitely
	client := &http.Client{Timeout: l.timeout}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch manifest: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrManifestFetchFailed, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	images, err := image.ExtractImagesFromManifest(string(body))
	if err != nil {
		return nil, fmt.Errorf("extract images from local-path-storage manifest: %w", err)
	}

	return images, nil
}

// installLocalPathProvisioner installs Rancher local-path-provisioner on Kind clusters.
func (l *Installer) installLocalPathProvisioner(ctx context.Context) error {
	// Apply the manifest using kubectl
	err := l.applyManifest(ctx)
	if err != nil {
		return fmt.Errorf("failed to apply manifest: %w", err)
	}

	// Wait for the deployment to be ready
	err = l.waitForReadiness(ctx)
	if err != nil {
		return fmt.Errorf("failed waiting for local-path-provisioner readiness: %w", err)
	}

	// Mark the local-path storage class as default
	err = l.setDefaultStorageClass(ctx)
	if err != nil {
		return fmt.Errorf("failed to set default storage class: %w", err)
	}

	return nil
}

// applyManifest applies the local-path-provisioner manifest using kubectl.
func (l *Installer) applyManifest(ctx context.Context) error {
	args := []string{"apply", "-f", localPathProvisionerManifestURL}

	if l.kubeconfig != "" {
		args = append([]string{"--kubeconfig", l.kubeconfig}, args...)
	}

	if l.context != "" {
		args = append([]string{"--context", l.context}, args...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %w, output: %s", err, string(output))
	}

	return nil
}

// waitForReadiness waits for the local-path-provisioner deployment to become ready.
func (l *Installer) waitForReadiness(ctx context.Context) error {
	clientset, err := k8s.NewClientset(l.kubeconfig, l.context)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	checks := []k8s.ReadinessCheck{
		{Type: "deployment", Namespace: "local-path-storage", Name: "local-path-provisioner"},
	}

	err = k8s.WaitForMultipleResources(ctx, clientset, checks, l.timeout)
	if err != nil {
		return fmt.Errorf("wait for local-path-provisioner deployment: %w", err)
	}

	return nil
}

// setDefaultStorageClass marks the local-path storage class as the default.
func (l *Installer) setDefaultStorageClass(ctx context.Context) error {
	clientset, err := k8s.NewClientset(l.kubeconfig, l.context)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Get the storage class
	storageClass, err := clientset.StorageV1().StorageClasses().Get(
		ctx,
		"local-path",
		metav1.GetOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to get storage class: %w", err)
	}

	// Add the default annotation
	if storageClass.Annotations == nil {
		storageClass.Annotations = make(map[string]string)
	}

	storageClass.Annotations["storageclass.kubernetes.io/is-default-class"] = "true"

	// Update the storage class
	_, err = clientset.StorageV1().StorageClasses().Update(
		ctx,
		storageClass,
		metav1.UpdateOptions{},
	)
	if err != nil {
		return fmt.Errorf("failed to update storage class: %w", err)
	}

	return nil
}
