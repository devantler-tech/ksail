package vclusterprovisioner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	vclusterconfig "github.com/loft-sh/vcluster/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

// defaultStorageClassAnnotation marks the cluster's default StorageClass.
const defaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"

// errPersistentStorageUnavailable is returned when the user's vcluster.yaml explicitly requests
// persistent storage but the host cluster has no default StorageClass to satisfy it.
var errPersistentStorageUnavailable = errors.New(
	"vcluster.yaml requests persistent storage but the host cluster has no default StorageClass; " +
		"install a default StorageClass or set controlPlane.statefulSet.persistence.volumeClaim.enabled: false",
)

// resolvePersistenceDisabled applies KSail's vCluster storage precedence and reports whether
// KSail must force an emptyDir-backed data volume (i.e. disable persistence).
//
// Precedence — most explicit first:
//  1. Explicit vcluster.yaml config wins. If it requests persistent storage
//     (volumeClaim.enabled: true or a storageClass is set) but the host has no default
//     StorageClass, fail fast: the user expects persistence and silently downgrading to
//     emptyDir would surprise them. An explicit volumeClaim.enabled: false is honored as-is.
//  2. Auto-detect. With no explicit request, keep vCluster's persistent default when the host
//     has a default StorageClass; otherwise fall back to emptyDir.
//  3. Default. With no StorageClass and no explicit request, disable persistence (emptyDir).
func resolvePersistenceDisabled(
	ctx context.Context,
	clientset kubernetes.Interface,
	userValuesPath string,
) (bool, error) {
	wantsPersistence, disablesPersistence, err := userPersistenceIntent(userValuesPath)
	if err != nil {
		return false, err
	}

	hasDefaultSC, err := hasDefaultStorageClass(ctx, clientset)
	if err != nil {
		return false, err
	}

	switch {
	case wantsPersistence && !hasDefaultSC:
		return false, errPersistentStorageUnavailable
	case wantsPersistence, disablesPersistence:
		// Explicit user choice — leave the user's values untouched (no KSail override).
		return false, nil
	case hasDefaultSC:
		// No explicit choice but storage is available — keep vCluster's persistent default.
		return false, nil
	default:
		// No explicit choice and no default StorageClass — force emptyDir.
		return true, nil
	}
}

// hasDefaultStorageClass reports whether the host cluster has a StorageClass annotated as the
// cluster default.
func hasDefaultStorageClass(ctx context.Context, clientset kubernetes.Interface) (bool, error) {
	list, err := clientset.StorageV1().StorageClasses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("list storage classes: %w", err)
	}

	for idx := range list.Items {
		if list.Items[idx].Annotations[defaultStorageClassAnnotation] == "true" {
			return true, nil
		}
	}

	return false, nil
}

// userPersistenceIntent parses the user's vcluster.yaml (if any) and reports whether it
// explicitly requests persistent storage (volumeClaim.enabled: true or a storageClass) or
// explicitly disables it (volumeClaim.enabled: false). An empty path or a path that no longer
// exists yields no intent so the auto-detection path is used.
func userPersistenceIntent(userValuesPath string) (bool, bool, error) {
	if strings.TrimSpace(userValuesPath) == "" {
		return false, false, nil
	}

	//nolint:gosec // user-provided vcluster values path, already resolved by configmanager
	data, err := os.ReadFile(userValuesPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}

		return false, false, fmt.Errorf("read vcluster values %q: %w", userValuesPath, err)
	}

	var probe struct {
		ControlPlane struct {
			StatefulSet struct {
				Persistence struct {
					VolumeClaim struct {
						Enabled      vclusterconfig.StrBool `json:"enabled"`
						StorageClass string                 `json:"storageClass"`
					} `json:"volumeClaim"`
				} `json:"persistence"`
			} `json:"statefulSet"`
		} `json:"controlPlane"`
	}

	err = yaml.Unmarshal(data, &probe)
	if err != nil {
		return false, false, fmt.Errorf("parse vcluster values %q: %w", userValuesPath, err)
	}

	claim := probe.ControlPlane.StatefulSet.Persistence.VolumeClaim
	wants := claim.Enabled.Bool() || strings.TrimSpace(claim.StorageClass) != ""
	disables := strings.EqualFold(strings.TrimSpace(string(claim.Enabled)), "false")

	return wants, disables, nil
}
