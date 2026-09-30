package talosprovisioner

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

const autoscalerImagePendingAnnotation = "ksail.io/autoscaler-image-rollout-pending"

var errAutoscalerImageBaselineChanged = errors.New(
	"autoscaler image baseline changed during convergence",
)

var errAutoscalerSnapshotImageUnavailable = errors.New("autoscaler snapshot image is unavailable")

func autoscalerImageChanged(changed, pending bool, previousImage, desiredImage string) bool {
	return pending || (changed && previousImage != "" && previousImage != desiredImage)
}

// mergeAutoscalerSecretData records pending image convergence in the same Secret
// update as the desired boot image. Saving the new template alone cannot prove
// that running nodes adopted it; a failed restart or recycle must survive a fresh
// invocation whose Secret and static-node diff are already unchanged.
func mergeAutoscalerSecretData(secret *corev1.Secret, desiredData map[string][]byte) bool {
	previousImage, previousErr := snapshotImageIDFromSecret(secret)
	if !k8s.MergeSecretData(secret, desiredData) {
		return false
	}

	desiredImage, desiredErr := snapshotImageIDFromSecret(secret)
	if previousErr == nil && desiredErr == nil && previousImage != desiredImage {
		if secret.Annotations == nil {
			secret.Annotations = make(map[string]string)
		}

		secret.Annotations[autoscalerImagePendingAnnotation] = desiredImage
	}

	return true
}

// completeAutoscalerImageBaseline acknowledges successful node convergence. A
// concurrent desired-image change is left pending and fails this invocation;
// unrelated Secret data and annotations are preserved by the conflict retry.
func (p *Provisioner) completeAutoscalerImageBaseline(ctx context.Context, imageID string) error {
	kubeclient, err := p.newSecretKubeclient("autoscaler image convergence")
	if err != nil {
		return err
	}

	secrets := kubeclient.CoreV1().Secrets(autoscalerConfigSecretNamespace)

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		secret, getErr := secrets.Get(ctx, autoscalerConfigSecretName, metav1.GetOptions{})
		if getErr != nil {
			return fmt.Errorf("reading converged autoscaler Secret: %w", getErr)
		}

		currentImage, decodeErr := snapshotImageIDFromSecret(secret)
		if decodeErr != nil {
			return decodeErr
		}

		pendingImage, pending := secret.Annotations[autoscalerImagePendingAnnotation]
		if currentImage != imageID || (pending && pendingImage != imageID) {
			return errAutoscalerImageBaselineChanged
		}

		if !pending {
			return nil
		}

		delete(secret.Annotations, autoscalerImagePendingAnnotation)

		_, updateErr := secrets.Update(ctx, secret, metav1.UpdateOptions{})
		if updateErr != nil {
			return fmt.Errorf("clearing pending autoscaler image rollout: %w", updateErr)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("acknowledging autoscaler image convergence: %w", err)
	}

	return nil
}
