package argocd

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

var errNilContext = errors.New("context is nil")

var errRepositoryURLRequired = errors.New("repository url is required")

// ManagerImpl implements the Argo CD GitOps manager.
type ManagerImpl struct {
	clientset kubernetes.Interface
	dynamic   dynamic.Interface
}

var _ Manager = (*ManagerImpl)(nil)

// NewManager creates a manager using provided Kubernetes clients.
//
// This is the primary constructor for unit tests.
func NewManager(clientset kubernetes.Interface, dyn dynamic.Interface) *ManagerImpl {
	return &ManagerImpl{clientset: clientset, dynamic: dyn}
}

// NewManagerFromKubeconfig creates a manager by building Kubernetes clients from kubeconfig.
func NewManagerFromKubeconfig(kubeconfig string) (*ManagerImpl, error) {
	restConfig, err := k8s.BuildRESTConfig(kubeconfig, "")
	if err != nil {
		return nil, fmt.Errorf("build rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return NewManager(clientset, dyn), nil
}

// Ensure creates or updates the Argo CD repository secret and Application.
func (m *ManagerImpl) Ensure(ctx context.Context, opts EnsureOptions) error {
	if ctx == nil {
		return errNilContext
	}

	if opts.RepositoryURL == "" {
		return errRepositoryURLRequired
	}

	if opts.TargetRevision == "" {
		opts.TargetRevision = "dev"
	}

	err := m.ensureNamespace(ctx, argoCDNamespace)
	if err != nil {
		return err
	}

	err = m.upsertRepositorySecret(ctx, repositorySecretOptions{
		repositoryURL: opts.RepositoryURL,
		username:      opts.Username,
		password:      opts.Password,
		insecure:      opts.Insecure,
	})
	if err != nil {
		return err
	}

	return m.upsertApplication(ctx, opts)
}

// UpdateTargetRevision updates the Application target revision and optionally requests a hard refresh.
func (m *ManagerImpl) UpdateTargetRevision(
	ctx context.Context,
	opts UpdateTargetRevisionOptions,
) error {
	if ctx == nil {
		return errNilContext
	}

	name := opts.ApplicationName
	if name == "" {
		name = defaultApplicationName
	}

	obj, err := m.dynamic.Resource(applicationGVR()).
		Namespace(argoCDNamespace).
		Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get Argo CD Application %s: %w", name, err)
	}

	if opts.TargetRevision != "" {
		err := unstructured.SetNestedField(
			obj.Object,
			opts.TargetRevision,
			"spec",
			"source",
			"targetRevision",
		)
		if err != nil {
			return fmt.Errorf("set Application.spec.source.targetRevision: %w", err)
		}
	}

	if opts.HardRefresh {
		annotations := obj.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}

		annotations[argoCDRefreshAnnotationKey] = argoCDHardRefreshAnnotation
		obj.SetAnnotations(annotations)
	}

	apps := m.dynamic.Resource(applicationGVR()).Namespace(argoCDNamespace)

	_, err = apps.Update(ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update Argo CD Application %s: %w", name, err)
	}

	return nil
}

func (m *ManagerImpl) ensureNamespace(ctx context.Context, name string) error {
	_, err := m.clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}

	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get namespace %s: %w", name, err)
	}

	_, err = m.clientset.CoreV1().
		Namespaces().
		Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", name, err)
	}

	return nil
}

func (m *ManagerImpl) upsertRepositorySecret(ctx context.Context, opts repositorySecretOptions) error {
	desired := buildRepositorySecret(opts)
	secrets := m.clientset.CoreV1().Secrets(argoCDNamespace)

	existing, err := secrets.Get(ctx, repositorySecretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = secrets.Create(ctx, desired, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("create repository secret: %w", err)
			}

			return nil
		}

		return fmt.Errorf("get repository secret: %w", err)
	}

	desired.ResourceVersion = existing.ResourceVersion

	_, err = secrets.Update(ctx, desired, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update repository secret: %w", err)
	}

	return nil
}

func (m *ManagerImpl) upsertApplication(ctx context.Context, opts EnsureOptions) error {
	desired := buildApplication(opts)
	name := desired.GetName()
	apps := m.dynamic.Resource(applicationGVR()).Namespace(argoCDNamespace)

	existing, err := apps.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			_, err = apps.Create(ctx, desired, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("create Argo CD Application: %w", err)
			}

			return nil
		}

		return fmt.Errorf("get Argo CD Application: %w", err)
	}

	desired.SetResourceVersion(existing.GetResourceVersion())

	_, err = apps.Update(ctx, desired, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("update Argo CD Application: %w", err)
	}

	return nil
}
