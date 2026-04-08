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

	switch opts.TenantType {
	case TypeFlux:
		readmeContent = fmt.Sprintf(
			"# %s\n\nFlux-managed tenant. "+
				"Resources in `k8s/` are automatically reconciled.\n",
			opts.Name)
	case TypeArgoCD:
		readmeContent = fmt.Sprintf(
			"# %s\n\nArgoCD-managed tenant. "+
				"Resources in `k8s/` are automatically synced.\n",
			opts.Name)
	case TypeKubectl:
		readmeContent = fmt.Sprintf(
			"# %s\n\n## Apply\n\n```\nkubectl apply -k k8s/\n```\n",
			opts.Name)
	default:
		readmeContent = fmt.Sprintf(
			"# %s\n\nKSail-managed tenant.\n",
			opts.Name)
	}

	return map[string][]byte{
		"README.md":              []byte(readmeContent),
		"k8s/kustomization.yaml": []byte(kustomizationContent),
	}
}
