package environment

import (
	"fmt"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/generator"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	ktypes "sigs.k8s.io/kustomize/api/types"
)

// KustomizationGenerator renders a kustomization model to a file. It is the
// scaffolder's kustomization generator interface
// ([generator.Generator[*ktypes.Kustomization, yamlgenerator.Options]]), named
// here so the multi-cluster writer takes exactly the dependency the single-cluster
// scaffolder already holds — the two render the same way, keeping their output
// byte-consistent.
type KustomizationGenerator = generator.Generator[*ktypes.Kustomization, yamlgenerator.Options]

// WriteMultiClusterLayout renders each file of a derived multi-cluster layout
// under sourceDir and returns the resolved output paths in layout order. It is the
// filesystem counterpart to [DeriveMultiClusterLayout]: derive is pure so the
// layout can be asserted in isolation, and write threads that layout through the
// scaffolder's kustomization generator so a multi-cluster scaffold stays
// byte-consistent with a single-cluster one.
//
// gen is the scaffolder's kustomization generator; sourceDir is the resolved
// GitOps source directory each [LayoutFile.RelPath] is joined onto. The generator
// creates any missing parent directories (the nested clusters/<env>/ path), so no
// directory is pre-created here. When force is false an existing file is left
// untouched — the scaffolder's idempotent behaviour — and its path is still
// reported; when force is true it is overwritten. A render error is wrapped with
// the failing file's relative path and aborts before a partial layout is
// reported.
func WriteMultiClusterLayout(
	gen KustomizationGenerator,
	sourceDir string,
	files []LayoutFile,
	force bool,
) ([]string, error) {
	written := make([]string, 0, len(files))

	for _, file := range files {
		output := filepath.Join(sourceDir, filepath.FromSlash(file.RelPath))

		_, err := gen.Generate(file.Kustomization, yamlgenerator.Options{
			Output: output,
			Force:  force,
		})
		if err != nil {
			return nil, fmt.Errorf("write multi-cluster layout file %q: %w", file.RelPath, err)
		}

		written = append(written, output)
	}

	return written, nil
}
