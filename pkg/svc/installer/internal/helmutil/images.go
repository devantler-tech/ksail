package helmutil

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	"github.com/devantler-tech/ksail/v5/pkg/svc/image"
)

// ImagesFromChart templates a Helm chart and extracts the container images from
// the rendered manifest. It is a shared helper used by Helm-based installers to
// implement the Installer.Images method.
func ImagesFromChart(
	ctx context.Context,
	client helm.Interface,
	spec *helm.ChartSpec,
) ([]string, error) {
	manifest, err := client.TemplateChart(ctx, spec)
	if err != nil {
		return nil, fmt.Errorf("template chart %s: %w", spec.ChartName, err)
	}

	images, err := image.ExtractImagesFromManifest(manifest)
	if err != nil {
		return nil, fmt.Errorf("extract images from %s manifest: %w", spec.ChartName, err)
	}

	return images, nil
}
