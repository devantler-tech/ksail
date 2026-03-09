package helm

import (
	"fmt"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

func executeAndExtractRelease(
	runFn func() (any, error),
) (*v1.Release, error) {
	releaser, err := runFn()
	if err != nil {
		return nil, err
	}

	rel, ok := releaser.(*v1.Release)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errUnexpectedReleaseType, releaser)
	}

	return rel, nil
}

func releaseToInfo(rel *v1.Release) *ReleaseInfo {
	if rel == nil {
		return nil
	}

	return &ReleaseInfo{
		Name:       rel.Name,
		Namespace:  rel.Namespace,
		Revision:   rel.Version,
		Status:     rel.Info.Status.String(),
		Chart:      rel.Chart.Metadata.Name,
		AppVersion: rel.Chart.Metadata.AppVersion,
		Updated:    rel.Info.LastDeployed,
		Notes:      rel.Info.Notes,
	}
}
