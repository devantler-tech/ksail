package scaffolder_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/stretchr/testify/assert"
)

const (
	vcFeatureGatesFlag = "--feature-gates=MutatingAdmissionPolicy=true"
	vcRuntimeConfigArg = "--runtime-config=admissionregistration.k8s.io/v1beta1=true"
	vcIssuerArg        = "--oidc-issuer-url=https://issuer.example.com"
	vcClientArg        = "--oidc-client-id=ksail"
)

type vcAPIArgsCase struct {
	featureGates bool
	oidc         *v1alpha1.OIDCSpec
	empty        bool
	contains     []string
	notContains  []string
}

func vcAPIServerArgsCases() map[string]vcAPIArgsCase {
	enabledOIDC := &v1alpha1.OIDCSpec{IssuerURL: "https://issuer.example.com", ClientID: "ksail"}
	oidcWithCA := &v1alpha1.OIDCSpec{
		IssuerURL: "https://issuer.example.com",
		ClientID:  "ksail",
		CAFile:    "/some/host/ca.crt",
	}

	return map[string]vcAPIArgsCase{
		"neither feature gates nor OIDC -> no block (nil)": {false, nil, true, nil, nil},
		"neither feature gates nor OIDC -> no block (empty)": {
			false,
			&v1alpha1.OIDCSpec{},
			true,
			nil,
			nil,
		},
		"feature gates only": {
			true, &v1alpha1.OIDCSpec{}, false,
			[]string{vcFeatureGatesFlag, vcRuntimeConfigArg},
			[]string{"--oidc-"},
		},
		"OIDC only": {
			false, enabledOIDC, false,
			[]string{vcIssuerArg, vcClientArg},
			[]string{vcFeatureGatesFlag},
		},
		"feature gates and OIDC merge into one block": {
			true, enabledOIDC, false,
			[]string{vcFeatureGatesFlag, vcRuntimeConfigArg, vcIssuerArg, vcClientArg},
			nil,
		},
		"CA file maps to in-container OIDC CA path": {
			false, oidcWithCA, false,
			[]string{"--oidc-ca-file=" + v1alpha1.OIDCCAContainerPath},
			[]string{"/some/host/ca.crt"},
		},
	}
}

func TestBuildVClusterAPIServerArgs(t *testing.T) {
	t.Parallel()

	for name, testCase := range vcAPIServerArgsCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			got := scaffolder.BuildVClusterAPIServerArgsForTest(
				testCase.featureGates,
				testCase.oidc,
			)

			if testCase.empty {
				assert.Empty(t, got)

				return
			}

			for _, want := range testCase.contains {
				assert.Contains(t, got, want)
			}

			for _, unwanted := range testCase.notContains {
				assert.NotContains(t, got, unwanted)
			}

			// A single apiServer/extraArgs block — duplicate keys would be invalid YAML.
			assert.Equal(t, 1, strings.Count(got, "apiServer:"), "exactly one apiServer key")
			assert.Equal(t, 1, strings.Count(got, "extraArgs:"), "exactly one extraArgs key")
		})
	}
}
