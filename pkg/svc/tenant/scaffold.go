package tenant

import "fmt"

const kustomizationContent = `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources: []
`

// ScaffoldFiles returns the files to push to a new tenant repository.
// The returned map is filename -> content (as bytes).
func ScaffoldFiles(opts Options) map[string][]byte {
	var readmeContent string

	srcDir := opts.SourceDirectory
	if srcDir == "" {
		srcDir = DefaultSourceDirectory
	}

	switch opts.TenantType {
	case TypeFlux:
		readmeContent = fmt.Sprintf(
			"# %s\n\nFlux-managed tenant. "+
				"Resources in `%s/` are automatically reconciled.\n",
			opts.Name, srcDir)
	case TypeArgoCD:
		readmeContent = fmt.Sprintf(
			"# %s\n\nArgoCD-managed tenant. "+
				"Resources in `%s/` are automatically synced.\n",
			opts.Name, srcDir)
	case TypeKubectl:
		readmeContent = fmt.Sprintf(
			"# %s\n\n## Apply\n\n```\nkubectl apply -k %s/\n```\n",
			opts.Name, srcDir)
	default:
		readmeContent = fmt.Sprintf(
			"# %s\n\nKSail-managed tenant.\n",
			opts.Name)
	}

	return map[string][]byte{
		"README.md":                            []byte(readmeContent),
		srcDir + "/kustomization.yaml": []byte(kustomizationContent),
	}
}
