package tenant

import (
	"fmt"
	"testing"

	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestScaffoldFiles(t *testing.T) {
	t.Parallel()

	types := []struct {
		tenantType TenantType
		readmeHas  string
	}{
		{TenantTypeKubectl, "kubectl apply"},
		{TenantTypeFlux, "Flux-managed"},
		{TenantTypeArgoCD, "ArgoCD-managed"},
	}

	for _, tt := range types {
		t.Run(fmt.Sprintf("%s has expected keys", tt.tenantType), func(t *testing.T) {
			t.Parallel()

			files := ScaffoldFiles(Options{Name: "my-tenant", TenantType: tt.tenantType})

			require.Contains(t, files, "README.md")
			require.Contains(t, files, "k8s/kustomization.yaml")
			require.Len(t, files, 2)
		})

		t.Run(fmt.Sprintf("%s README mentions %s", tt.tenantType, tt.readmeHas), func(t *testing.T) {
			t.Parallel()

			files := ScaffoldFiles(Options{Name: "my-tenant", TenantType: tt.tenantType})

			require.Contains(t, string(files["README.md"]), tt.readmeHas)
		})
	}
}

func TestScaffoldFilesKustomizationValid(t *testing.T) {
	t.Parallel()

	for _, tt := range []TenantType{TenantTypeKubectl, TenantTypeFlux, TenantTypeArgoCD} {
		t.Run(string(tt), func(t *testing.T) {
			t.Parallel()

			files := ScaffoldFiles(Options{Name: "my-tenant", TenantType: tt})
			content := string(files["k8s/kustomization.yaml"])

			require.Contains(t, content, "apiVersion:")
			require.Contains(t, content, "kind:")
		})
	}
}

func TestScaffoldFilesSnapshots(t *testing.T) {
	t.Parallel()

	for _, tt := range []TenantType{TenantTypeKubectl, TenantTypeFlux, TenantTypeArgoCD} {
		t.Run(string(tt), func(t *testing.T) {
			t.Parallel()

			files := ScaffoldFiles(Options{Name: "my-tenant", TenantType: tt})

			snaps.MatchSnapshot(t, string(files["README.md"]))
			snaps.MatchSnapshot(t, string(files["k8s/kustomization.yaml"]))
		})
	}
}
