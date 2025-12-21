package localpathstorageinstaller

import (
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// localPathProvisionerVersion is the version of Rancher local-path-provisioner to install.
	localPathProvisionerVersion = "v0.0.32"
	// localPathProvisionerManifestURL is the URL to the local-path-provisioner manifest.
	localPathProvisionerManifestURL = "https://raw.githubusercontent.com/rancher/local-path-provisioner/" +
		localPathProvisionerVersion + "/deploy/local-path-storage.yaml"
)

// LocalPathStorageInstaller installs local-path-provisioner on Kind clusters.
type LocalPathStorageInstaller struct {
	kubeconfig   string
	context      string
	timeout      time.Duration
	distribution v1alpha1.Distribution
}

// NewLocalPathStorageInstaller creates a new local-path-storage installer instance.
func NewLocalPathStorageInstaller(
	kubeconfig, context string,
	timeout time.Duration,
	distribution v1alpha1.Distribution,
) *LocalPathStorageInstaller {
	return &LocalPathStorageInstaller{
		kubeconfig:   kubeconfig,
		context:      context,
		timeout:      timeout,
		distribution: distribution,
	}
}

// Install installs local-path-provisioner if needed based on the distribution.
func (l *LocalPathStorageInstaller) Install(ctx context.Context) error {
	// K3d already has local-path-provisioner by default, no action needed
	if l.distribution.ProvidesStorageByDefault() {
		return nil
	}

	// For Kind, install local-path-provisioner
	if l.distribution == v1alpha1.DistributionKind {
		return l.installLocalPathProvisioner(ctx)
	}

	return nil
}

// Uninstall is a no-op as we don't support uninstalling storage provisioners.
func (l *LocalPathStorageInstaller) Uninstall(_ context.Context) error {
	return nil
}

// installLocalPathProvisioner installs Rancher local-path-provisioner on Kind clusters.
func (l *LocalPathStorageInstaller) installLocalPathProvisioner(ctx context.Context) error {
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
func (l *LocalPathStorageInstaller) applyManifest(ctx context.Context) error {
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

// createClientset creates a Kubernetes clientset for the configured cluster.
func (l *LocalPathStorageInstaller) createClientset() (*kubernetes.Clientset, error) {
	restConfig, err := k8s.BuildRESTConfig(l.kubeconfig, l.context)
	if err != nil {
		return nil, fmt.Errorf("failed to build rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, nil
}

// waitForReadiness waits for the local-path-provisioner deployment to become ready.
func (l *LocalPathStorageInstaller) waitForReadiness(ctx context.Context) error {
	clientset, err := l.createClientset()
	if err != nil {
		return err
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
func (l *LocalPathStorageInstaller) setDefaultStorageClass(ctx context.Context) error {
	clientset, err := l.createClientset()
	if err != nil {
		return err
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
