package workload_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericiooptions"
	"sigs.k8s.io/yaml"
)

const podinfoGit = "--url=https://github.com/stefanprodan/podinfo"

type fluxCreateCase struct {
	name, apiVersion, kind, namespace string
	args                              []string
}

func fluxCreateCases() []fluxCreateCase {
	return []fluxCreateCase{
		{
			name: "source-git-default-namespace", apiVersion: "source.toolkit.fluxcd.io/v1",
			kind: "GitRepository", namespace: "flux-system",
			args: []string{"source", "git", "podinfo", podinfoGit, "--branch=master"},
		},
		{
			name: "source-git-short-namespace", apiVersion: "source.toolkit.fluxcd.io/v1",
			kind: "GitRepository", namespace: "apps",
			args: []string{"source", "git", "podinfo", "-n", "apps", podinfoGit, "--branch=master"},
		},
		{
			name: "source-helm", apiVersion: "source.toolkit.fluxcd.io/v1",
			kind: "HelmRepository", namespace: "apps",
			args: []string{
				"source", "helm", "podinfo", "--namespace=apps",
				"--url=https://stefanprodan.github.io/podinfo",
			},
		},
		{
			name: "source-oci", apiVersion: "source.toolkit.fluxcd.io/v1",
			kind: "OCIRepository", namespace: "flux-system",
			args: []string{
				"source", "oci", "podinfo",
				"--url=oci://ghcr.io/stefanprodan/manifests/podinfo", "--tag=latest",
			},
		},
		{
			name:       "kustomization",
			apiVersion: "kustomize.toolkit.fluxcd.io/v1",
			kind:       "Kustomization",
			namespace:  "flux-system",
			args: []string{
				"kustomization",
				"podinfo",
				"--source=GitRepository/podinfo",
				"--path=./kustomize",
			},
		},
		{
			name: "helmrelease", apiVersion: "helm.toolkit.fluxcd.io/v2",
			kind: "HelmRelease", namespace: "apps",
			args: []string{
				"helmrelease", "podinfo", "-n", "apps",
				"--source=HelmRepository/podinfo", "--chart=podinfo",
			},
		},
	}
}

// TestCreateFluxSubcommandsExportThroughWorkloadCreate drives every Flux create sub-command
// through the real `workload create` tree, where they are moved under kubectl's create
// command. They once lost --namespace there and panicked, and --export dropped apiVersion
// and kind (ksail#8190).
func TestCreateFluxSubcommandsExportThroughWorkloadCreate(t *testing.T) {
	t.Parallel()

	for _, testCase := range fluxCreateCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var out, errOut bytes.Buffer

			cmd := workload.NewCreateCmdWithStreams(genericiooptions.IOStreams{
				In: &bytes.Buffer{}, Out: &out, ErrOut: &errOut,
			})
			cmd.SetOut(&out)
			cmd.SetErr(&errOut)
			cmd.SetArgs(append(testCase.args, "--export"))

			require.NotPanics(t, func() { require.NoError(t, cmd.Execute(), errOut.String()) })

			var manifest struct {
				APIVersion string `json:"apiVersion"`
				Kind       string `json:"kind"`
				Metadata   struct {
					Name      string `json:"name"`
					Namespace string `json:"namespace"`
				} `json:"metadata"`
			}

			require.NoError(t, yaml.Unmarshal(out.Bytes(), &manifest), out.String())
			require.Equal(t, testCase.apiVersion, manifest.APIVersion, out.String())
			require.Equal(t, testCase.kind, manifest.Kind)
			require.Equal(t, "podinfo", manifest.Metadata.Name)
			require.Equal(t, testCase.namespace, manifest.Metadata.Namespace)
		})
	}
}
