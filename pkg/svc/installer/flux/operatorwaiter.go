package fluxinstaller

import (
	"context"
	"errors"
	"fmt"
	"time"

	fluxclient "github.com/devantler-tech/ksail/v7/pkg/client/flux"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// fluxOperatorDeploymentName is the Deployment the flux-operator Helm chart
// installs; it is the reconciler that turns the FluxInstance into the Flux
// controllers and their CRDs (source.toolkit.fluxcd.io etc.).
const fluxOperatorDeploymentName = "flux-operator"

var errOperatorNotAvailable = errors.New(
	"flux-operator deployment is not yet available",
)

// newKubernetesClient creates a typed clientset for reading the operator
// Deployment.
//
//nolint:gochecknoglobals // Allows mocking for tests
var newKubernetesClient = func(restConfig *rest.Config) (kubernetes.Interface, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return clientset, nil
}

// waitForOperatorAvailable waits for the flux-operator Deployment to report
// Available=True before callers start polling for the APIs the operator
// installs. The source.toolkit.fluxcd.io group only appears after the
// operator reconciles the FluxInstance, so without this gate a slow or
// crash-looping operator start silently consumes the whole API availability
// budget and the eventual timeout blames the wrong phase (issue #5597).
// On timeout the error names this phase and appends pod diagnostics from the
// flux-system namespace so the starved stage is attributable from the error
// alone.
func waitForOperatorAvailable(
	ctx context.Context,
	restConfig *rest.Config,
	timeout, interval time.Duration,
) error {
	err := pollUntilReady(
		ctx,
		timeout,
		interval,
		"Deployment "+fluxclient.DefaultNamespace+"/"+fluxOperatorDeploymentName+
			" to be Available",
		func() (bool, error) {
			return checkOperatorAvailable(ctx, restConfig)
		},
	)
	if err != nil {
		diag := diagnoseFluxPodFailures(ctx, restConfig)

		return fmt.Errorf(
			"flux-operator did not become available (Flux CRDs are installed by"+
				" the operator, so its APIs cannot appear until it runs): %w%s",
			err, diag,
		)
	}

	return nil
}

// checkOperatorAvailable performs one availability probe of the operator
// Deployment. Get errors (incl. not-found while the Helm release is still
// settling) are reported for timeout attribution but treated as retryable.
func checkOperatorAvailable(ctx context.Context, restConfig *rest.Config) (bool, error) {
	clientset, err := newKubernetesClient(restConfig)
	if err != nil {
		return false, err
	}

	deployment, err := clientset.AppsV1().
		Deployments(fluxclient.DefaultNamespace).
		Get(ctx, fluxOperatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to get flux-operator deployment: %w", err)
	}

	for _, condition := range deployment.Status.Conditions {
		if condition.Type == appsv1.DeploymentAvailable &&
			condition.Status == corev1.ConditionTrue {
			return true, nil
		}
	}

	return false, errOperatorNotAvailable
}
