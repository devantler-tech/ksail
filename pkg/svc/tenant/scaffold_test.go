package tenant_test

import (
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/tenant"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
)

func TestScaffoldFiles(t *testing.T) {
	t.Parallel()

	types := []struct {
		tenantType tenant.Type
		readmeHas  string
	}{
		{tenant.TypeKubectl, "kubectl apply"},
		{tenant.TypeFlux, "Flux-managed"},
		{tenant.TypeArgoCD, "ArgoCD-managed"},
	}

	for _, testCase := range types {
		t.Run(fmt.Sprintf("%s has expected keys", testCase.tenantType), func(t *testing.T) {
			t.Parallel()

			files := tenant.ScaffoldFiles(tenant.Options{
				Name:       "my-tenant",
				TenantType: testCase.tenantType,
			})

			require.Contains(t, files, "README.md")
			require.Contains(t, files, "k8s/kustomization.yaml")
			require.Len(t, files, 2)
		})

		t.Run(
			fmt.Sprintf("%s README mentions %s", testCase.tenantType, testCase.readmeHas),
			func(t *testing.T) {
				t.Parallel()

				files := tenant.ScaffoldFiles(tenant.Options{
					Name:       "my-tenant",
					TenantType: testCase.tenantType,
				})

				require.Contains(t, string(files["README.md"]), testCase.readmeHas)
			},
		)
	}
}

func TestScaffoldFilesKustomizationValid(t *testing.T) {
	t.Parallel()

	for _, testCase := range []tenant.Type{
		tenant.TypeKubectl,
		tenant.TypeFlux,
		tenant.TypeArgoCD,
	} {
		t.Run(string(testCase), func(t *testing.T) {
			t.Parallel()

			files := tenant.ScaffoldFiles(tenant.Options{
				Name:       "my-tenant",
				TenantType: testCase,
			})
			content := string(files["k8s/kustomization.yaml"])

			require.Contains(t, content, "apiVersion:")
			require.Contains(t, content, "kind:")
		})
	}
}

func TestScaffoldFilesSnapshots(t *testing.T) {
	t.Parallel()

	for _, testCase := range []tenant.Type{
		tenant.TypeKubectl,
		tenant.TypeFlux,
		tenant.TypeArgoCD,
	} {
		t.Run(string(testCase), func(t *testing.T) {
			t.Parallel()

			files := tenant.ScaffoldFiles(tenant.Options{
				Name:       "my-tenant",
				TenantType: testCase,
			})

			snaps.MatchSnapshot(t, string(files["README.md"]))
			snaps.MatchSnapshot(t, string(files["k8s/kustomization.yaml"]))
		})
	}
}
