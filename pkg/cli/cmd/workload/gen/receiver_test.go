package gen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload/gen"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

const githubIssuer = "--oidc-issuer=https://token.actions.githubusercontent.com"

func TestGenReceiverGenericOIDCNeedsNoSecret(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewReceiverCmd, []string{
		"github-actions",
		"--type=generic-oidc",
		githubIssuer,
		"--oidc-audience=flux",
		"--oidc-validation=claims.repository_owner == 'my-org'",
		"--oidc-validation=claims.ref == 'refs/heads/main'",
		"--oidc-validation-message=only my-org may trigger a reconcile",
		"--oidc-validation-message=only main may trigger a reconcile",
		"--resource=GitRepository/webapp",
	})

	require.NoError(t, err)
	require.NotContains(t, output, "secretRef")
	snaps.MatchSnapshot(t, output)
}

func TestGenReceiverWithSecretToken(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewReceiverCmd, []string{
		"github-push",
		"--namespace=apps",
		"--type=github",
		"--events=push,ping",
		"--secret-ref=webhook-token",
		"--resource=GitRepository/webapp.apps",
	})

	require.NoError(t, err)
	require.NotContains(t, output, "oidcProviders")
	snaps.MatchSnapshot(t, output)
}

// TestGenReceiverFilteredResourcesRoundTrip checks that each --resource's kind, name,
// namespace and CEL filter (commas included) and the top-level --resource-filter land in
// spec exactly as given, in order.
func TestGenReceiverFilteredResourcesRoundTrip(t *testing.T) {
	t.Parallel()

	output, _, err := execGen(t, gen.NewReceiverCmd, []string{
		"releases",
		"--type=generic-oidc",
		githubIssuer,
		"--oidc-validation=claims.repository_owner == 'my-org'",
		"--resource=OCIRepository/*=res.metadata.labels['repo'] == claims.repository",
		"--resource=HelmRelease/podinfo.apps=['main', 'release'].exists(b, b == req.ref)",
		"--resource=Kustomization/infra",
		"--resource-filter=res.metadata.namespace != 'kube-system'",
	})
	require.NoError(t, err)

	var receiver struct {
		Spec struct {
			ResourceFilter string `json:"resourceFilter"`
			Resources      []struct {
				Kind, Name, Namespace, Filter string
			} `json:"resources"`
		} `json:"spec"`
	}

	require.NoError(t, yaml.Unmarshal([]byte(output), &receiver))
	require.Equal(t, "res.metadata.namespace != 'kube-system'", receiver.Spec.ResourceFilter)
	require.Equal(t, []struct{ Kind, Name, Namespace, Filter string }{
		{
			Kind:   "OCIRepository",
			Name:   "*",
			Filter: "res.metadata.labels['repo'] == claims.repository",
		},
		{
			Kind: "HelmRelease", Name: "podinfo", Namespace: "apps",
			Filter: "['main', 'release'].exists(b, b == req.ref)",
		},
		{Kind: "Kustomization", Name: "infra"},
	}, receiver.Spec.Resources)
	snaps.MatchSnapshot(t, output)
}

type receiverRefusal struct {
	name, want string
	args       []string
}

func receiverRefusals() []receiverRefusal {
	resource := "--resource=GitRepository/webapp"
	validation := "--oidc-validation=claims.repository_owner == 'my-org'"

	return []receiverRefusal{
		{
			name: "missing-type",
			args: []string{resource, "--secret-ref=t"},
			want: "--type is required",
		},
		{name: "unknown-type", args: []string{"--type=jenkins", resource}, want: "invalid --type"},
		{
			name: "missing-resource",
			args: []string{"--type=github", "--secret-ref=t"},
			want: "at least one --resource",
		},
		{
			name: "unknown-kind",
			args: []string{"--type=github", "--secret-ref=t", "--resource=Deployment/web"},
			want: "invalid kind",
		},
		{
			name: "malformed-resource",
			args: []string{"--type=github", "--secret-ref=t", "--resource=webapp"},
			want: "invalid --resource",
		},
		{
			name: "secret-type-without-secret",
			args: []string{"--type=github", resource},
			want: "--secret-ref is required",
		},
		{
			name: "oidc-flags-on-secret-type",
			args: []string{"--type=github", "--secret-ref=t", githubIssuer, resource},
			want: "require --type=generic-oidc",
		},
		{
			name: "oidc-with-secret",
			args: []string{
				"--type=generic-oidc",
				githubIssuer,
				validation,
				"--secret-ref=t",
				resource,
			},
			want: "not used by --type=generic-oidc",
		},
		{
			name: "oidc-without-issuer",
			args: []string{"--type=generic-oidc", validation, resource},
			want: "--oidc-issuer",
		},
		{
			name: "oidc-non-url-issuer",
			args: []string{
				"--type=generic-oidc",
				"--oidc-issuer=token.actions",
				validation,
				resource,
			},
			want: "--oidc-issuer",
		},
		{
			name: "oidc-message-count",
			args: []string{
				"--type=generic-oidc", githubIssuer, validation, resource,
				"--oidc-validation-message=a", "--oidc-validation-message=b",
			},
			want: "one --oidc-validation-message per --oidc-validation",
		},
		{
			name: "oidc-without-validation",
			args: []string{"--type=generic-oidc", githubIssuer, resource},
			want: "at least one --oidc-validation",
		},
	}
}

func TestGenReceiverRejectsInvalidFlags(t *testing.T) {
	t.Parallel()

	for _, testCase := range receiverRefusals() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			output, _, err := execGen(
				t,
				gen.NewReceiverCmd,
				append([]string{"webhook"}, testCase.args...),
			)

			require.Error(t, err)
			require.ErrorContains(t, err, testCase.want)
			require.Empty(t, output, "a refused Receiver must not be written")
		})
	}
}
