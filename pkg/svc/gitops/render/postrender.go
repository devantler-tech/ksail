package render

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	fluxkustomize "github.com/fluxcd/pkg/kustomize"
	helmaction "helm.sh/helm/v4/pkg/action"
	"sigs.k8s.io/kustomize/api/types"
	"sigs.k8s.io/kustomize/kyaml/filesys"
)

const helmRenderedResource = "helm-output.yaml"

// Both Flux patch/image types use the same JSON keys as the native Kustomize
// types. Keep the renderer configuration restricted to these two inline inputs.
type helmKustomization struct {
	helmv2.Kustomize

	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Resources  []string `json:"resources"`
}

type fluxHelmPostRenderer []json.RawMessage

func configurePostRenderers(spec *helm.ChartSpec, release *helmv2.HelmRelease) (string, error) {
	var renderer fluxHelmPostRenderer

	for _, entry := range release.Spec.PostRenderers {
		if entry.Kustomize == nil {
			continue
		}

		cfg, err := json.Marshal(helmKustomization{
			APIVersion: types.KustomizationVersion,
			Kind:       types.KustomizationKind,
			Resources:  []string{helmRenderedResource},
			Kustomize:  *entry.Kustomize,
		})
		if err != nil {
			return "", fmt.Errorf("encode Kustomize settings: %w", err)
		}

		renderer = append(renderer, cfg)
	}

	if len(renderer) == 0 {
		return "", nil
	}

	strategy := release.GetPostRenderStrategy()

	key, err := json.Marshal(struct {
		Strategy  helmv2.PostRenderStrategy `json:"strategy"`
		Renderers fluxHelmPostRenderer      `json:"renderers"`
	}{
		Strategy:  strategy,
		Renderers: renderer,
	})
	if err != nil {
		return "", fmt.Errorf("encode post-render cache settings: %w", err)
	}

	spec.TemplatePostRenderer = renderer
	spec.TemplatePostRenderStrategy = helmaction.PostRenderStrategy(strategy)

	return string(key), nil
}

func (r fluxHelmPostRenderer) Run(manifests *bytes.Buffer) (*bytes.Buffer, error) {
	for index, config := range r {
		filesystem := filesys.MakeFsInMemory()

		err := filesystem.WriteFile(helmRenderedResource, manifests.Bytes())
		if err != nil {
			return nil, fmt.Errorf("prepare Helm post-renderer %d input: %w", index+1, err)
		}

		err = filesystem.WriteFile("kustomization.yaml", config)
		if err != nil {
			return nil, fmt.Errorf("prepare Helm post-renderer %d configuration: %w", index+1, err)
		}

		resources, err := fluxkustomize.Build(filesystem, ".")
		if err != nil {
			return nil, fmt.Errorf("apply Helm post-renderer %d: %w", index+1, err)
		}

		yaml, err := resources.AsYaml()
		if err != nil {
			return nil, fmt.Errorf("serialize Helm post-renderer %d output: %w", index+1, err)
		}

		manifests = bytes.NewBuffer(yaml)
	}

	return manifests, nil
}
