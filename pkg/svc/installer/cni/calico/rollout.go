package calicoinstaller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

const (
	// tigeraOperatorGroup is the API group of the Tigera operator resources.
	tigeraOperatorGroup = "operator.tigera.io"
	// operatorInstallationName is the Installation the operator chart creates.
	operatorInstallationName = "default"
	// calicoStatusName is the TigeraStatus the operator reports Calico's state in.
	calicoStatusName = "calico"
	// calicoNodeNamespace and calicoNodeName locate the calico-node DaemonSet.
	calicoNodeNamespace = "calico-system"
	calicoNodeName      = "calico-node"
)

// ErrRolloutIncomplete reports that Calico did not finish rolling out in time.
var ErrRolloutIncomplete = errors.New("calico rollout incomplete")

//nolint:gochecknoglobals // immutable resource identities
var (
	installationResource = schema.GroupVersionResource{
		Group: tigeraOperatorGroup, Version: "v1", Resource: "installations",
	}
	tigeraStatusResource = schema.GroupVersionResource{
		Group: tigeraOperatorGroup, Version: "v1", Resource: "tigerastatuses",
	}
)

// WaitForRollout waits until the Tigera operator has applied the current
// Installation and calico-node runs the result on every node (ksail#8281).
//
// Install returns once Helm has stored the new Installation, but the operator
// renders calico-node from it asynchronously, so straight after an upgrade the
// DaemonSet can still look ready with its old pod spec. The operator stamps the
// TigeraStatus conditions with the Installation generation only at the end of a
// reconcile, after it has applied the DaemonSet, and keeps Available false while
// that DaemonSet rolls out. So Calico has rolled out once the calico condition
// is Available for the current generation, nothing is progressing or degraded,
// and calico-node itself reports a complete rollout.
func (c *Installer) WaitForRollout(ctx context.Context) error {
	resources, core, _, err := c.prerequisiteClients()
	if err != nil {
		return err
	}

	return waitForRollout(ctx, resources, core, c.GetTimeout())
}

func waitForRollout(
	ctx context.Context,
	resources dynamic.Interface,
	core kubernetes.Interface,
	timeout time.Duration,
) error {
	pending := "nothing observed yet"

	err := readiness.PollForReadiness(ctx, timeout, func(ctx context.Context) (bool, error) {
		reason, err := rolloutPending(ctx, resources, core)
		if err != nil {
			return false, err
		}

		pending = reason

		return reason == "", nil
	})
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrRolloutIncomplete, pending, err)
	}

	return nil
}

// rolloutPending returns why Calico has not finished rolling out, or "" once it
// has. Objects the operator has not created yet keep the wait going; any other
// API error ends it.
func rolloutPending(
	ctx context.Context,
	resources dynamic.Interface,
	core kubernetes.Interface,
) (string, error) {
	installation, err := resources.Resource(installationResource).
		Get(ctx, operatorInstallationName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "Installation " + operatorInstallationName + " not found", nil
	}

	if err != nil {
		return "", fmt.Errorf("read Calico Installation: %w", err)
	}

	status, err := resources.Resource(tigeraStatusResource).
		Get(ctx, calicoStatusName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "TigeraStatus " + calicoStatusName + " not reported yet", nil
	}

	if err != nil {
		return "", fmt.Errorf("read Calico TigeraStatus: %w", err)
	}

	reason := operatorPending(status, installation.GetGeneration())
	if reason != "" {
		return reason, nil
	}

	daemonSet, err := core.AppsV1().DaemonSets(calicoNodeNamespace).
		Get(ctx, calicoNodeName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "DaemonSet " + calicoNodeNamespace + "/" + calicoNodeName + " not found", nil
	}

	if err != nil {
		return "", fmt.Errorf("read calico-node DaemonSet: %w", err)
	}

	return daemonSetPending(daemonSet), nil
}

// operatorPending reports why the calico TigeraStatus does not yet show the
// given Installation generation as applied and available.
func operatorPending(status *unstructured.Unstructured, generation int64) string {
	conditions, _, _ := unstructured.NestedSlice(status.Object, "status", "conditions")

	available := false

	for _, item := range conditions {
		condition, ok := item.(map[string]any)
		if !ok {
			continue
		}

		conditionType, _, _ := unstructured.NestedString(condition, "type")
		conditionStatus, _, _ := unstructured.NestedString(condition, "status")
		observed, _, _ := unstructured.NestedInt64(condition, "observedGeneration")

		switch conditionType {
		case "Available":
			if conditionStatus == string(metav1.ConditionTrue) && observed < generation {
				return fmt.Sprintf(
					"operator has not applied Installation generation %d yet (observed %d)",
					generation, observed,
				)
			}

			available = conditionStatus == string(metav1.ConditionTrue)
		case "Progressing", "Degraded":
			if conditionStatus == string(metav1.ConditionTrue) {
				message, _, _ := unstructured.NestedString(condition, "message")

				return fmt.Sprintf("Calico is %s: %s", conditionType, message)
			}
		}
	}

	if !available {
		return "Calico is not Available"
	}

	return ""
}

// daemonSetPending reports why calico-node has not finished rolling out its
// current pod spec, using the same conditions the Tigera operator applies.
func daemonSetPending(daemonSet *appsv1.DaemonSet) string {
	daemonStatus := daemonSet.Status

	switch {
	case daemonSet.Generation != daemonStatus.ObservedGeneration:
		return fmt.Sprintf("calico-node generation %d not observed yet (observed %d)",
			daemonSet.Generation, daemonStatus.ObservedGeneration)
	case daemonStatus.DesiredNumberScheduled == 0:
		return "calico-node has no scheduled pods"
	case daemonStatus.UpdatedNumberScheduled != daemonStatus.DesiredNumberScheduled ||
		daemonStatus.NumberUnavailable != 0 ||
		daemonStatus.NumberAvailable != daemonStatus.DesiredNumberScheduled ||
		daemonStatus.NumberReady != daemonStatus.DesiredNumberScheduled:
		return fmt.Sprintf("calico-node rollout: %d of %d updated, %d available, %d unavailable",
			daemonStatus.UpdatedNumberScheduled, daemonStatus.DesiredNumberScheduled,
			daemonStatus.NumberAvailable, daemonStatus.NumberUnavailable)
	default:
		return ""
	}
}
