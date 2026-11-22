package kyvernopolicy_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fluxController = "system:serviceaccount:flux-system:kustomize-controller"
	tenantAccount  = "system:serviceaccount:tenant-a:tenant-a"
)

func fluxIdentity() *kyvernopolicy.Identity {
	return &kyvernopolicy.Identity{
		Username: fluxController,
		Groups:   []string{"system:serviceaccounts", "system:serviceaccounts:flux-system"},
	}
}

func tenantIdentity() *kyvernopolicy.Identity {
	return &kyvernopolicy.Identity{
		Username: tenantAccount,
		Groups:   []string{"system:serviceaccounts", "system:serviceaccounts:tenant-a"},
	}
}

// onlyFluxPolicy denies a ConfigMap unless Flux's kustomize-controller applies it.
const onlyFluxPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: only-flux
spec:
  validationFailureAction: Enforce
  rules:
  - name: only-flux-applies
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    validate:
      message: "only Flux may apply ConfigMaps"
      deny:
        conditions:
          any:
          - key: "{{ request.userInfo.username }}"
            operator: NotEquals
            value: system:serviceaccount:flux-system:kustomize-controller
`

// fluxExemptPolicy requires a team label, except for documents Flux's
// kustomize-controller applies.
const fluxExemptPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: flux-exempt
spec:
  validationFailureAction: Enforce
  rules:
  - name: check-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    exclude:
      any:
      - subjects:
        - kind: ServiceAccount
          name: kustomize-controller
          namespace: flux-system
    validate:
      message: "label team is required"
      pattern:
        metadata:
          labels:
            team: "?*"
`

// fluxGroupExemptPolicy requires a team label, except for documents a member
// of the flux-system service account group applies.
const fluxGroupExemptPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: flux-group-exempt
spec:
  validationFailureAction: Enforce
  rules:
  - name: check-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    exclude:
      any:
      - subjects:
        - kind: Group
          name: system:serviceaccounts:flux-system
    validate:
      message: "label team is required"
      pattern:
        metadata:
          labels:
            team: "?*"
`

// tenantOnlyPolicy selects by subject alone: it applies to whatever the tenant
// service account applies.
const tenantOnlyPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: tenant-only
spec:
  validationFailureAction: Enforce
  rules:
  - name: check-team
    match:
      any:
      - subjects:
        - kind: ServiceAccount
          name: tenant-a
          namespace: tenant-a
    validate:
      message: "label team is required"
      pattern:
        metadata:
          labels:
            team: "?*"
`

// fluxExemptPlatformPolicy requires a team label in platform-labelled
// namespaces, except for documents Flux's kustomize-controller applies. For a
// document whose Namespace is not rendered, the exclusion still decides
// whether the rule could apply.
const fluxExemptPlatformPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: flux-exempt-platform
spec:
  validationFailureAction: Enforce
  rules:
  - name: check-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
          namespaceSelector:
            matchLabels:
              tier: platform
    exclude:
      any:
      - subjects:
        - kind: ServiceAccount
          name: kustomize-controller
          namespace: flux-system
    validate:
      message: "label team is required"
      pattern:
        metadata:
          labels:
            team: "?*"
`

type identityCase struct {
	name        string
	doc         map[string]any
	identity    *kyvernopolicy.Identity
	blocking    int
	errored     int
	unsupported int
}

// assertIdentityCase checks the outcome counts. blocking counts policy
// denials only, never evaluation errors, so an engine that fails to apply the
// policy cannot pass as one that rejects the requester.
func assertIdentityCase(t *testing.T, testCase identityCase, violations []kyvernopolicy.Violation) {
	t.Helper()

	blocking, errored, unsupported := 0, 0, 0

	for _, violation := range violations {
		switch {
		case violation.Error:
			errored++
		case violation.Blocking:
			blocking++
		}

		if violation.Unsupported {
			unsupported++
		}
	}

	assert.Len(t, violations, blocking+errored+unsupported, "%+v", violations)
	assert.Equal(t, testCase.blocking, blocking, "%+v", violations)
	assert.Equal(t, testCase.errored, errored, "%+v", violations)
	assert.Equal(t, testCase.unsupported, unsupported, "%+v", violations)
}

func classicIdentityCases() map[string][]identityCase {
	unlabelled := configMap("default", nil)
	unrendered := configMap("dev", nil)

	return map[string][]identityCase{
		onlyFluxPolicy: {
			{name: "flux is admitted", doc: unlabelled, identity: fluxIdentity()},
			{name: "tenant is rejected", doc: unlabelled, identity: tenantIdentity(), blocking: 1},
			// An empty requester has no username to read, so the rule errors.
			{name: "no identity is an empty requester", doc: unlabelled, errored: 1},
			{
				name:     "unselected document passes",
				doc:      secret("default"),
				identity: tenantIdentity(),
			},
		},
		fluxExemptPolicy: {
			{name: "excluded subject is admitted", doc: unlabelled, identity: fluxIdentity()},
			{
				name: "other subject is rejected", doc: unlabelled,
				identity: tenantIdentity(), blocking: 1,
			},
			{name: "no identity is an empty requester", doc: unlabelled, blocking: 1},
		},
		fluxGroupExemptPolicy: {
			{name: "excluded group is admitted", doc: unlabelled, identity: fluxIdentity()},
			{
				name: "other group is rejected", doc: unlabelled,
				identity: tenantIdentity(), blocking: 1,
			},
		},
		tenantOnlyPolicy: {
			{name: "unmatched subject is admitted", doc: unlabelled, identity: fluxIdentity()},
			{
				name: "matched subject is rejected", doc: unlabelled,
				identity: tenantIdentity(), blocking: 1,
			},
			{name: "no identity matches no subject", doc: unlabelled},
		},
		fluxExemptPlatformPolicy: {
			{
				name: "pre-match excludes the declared subject", doc: unrendered,
				identity: fluxIdentity(),
			},
			{
				name: "pre-match keeps another subject", doc: unrendered,
				identity: tenantIdentity(), unsupported: 1,
			},
			{name: "no identity is an empty requester", doc: unrendered, unsupported: 1},
		},
	}
}

func TestEvaluateAs_ClassicPolicyReadingIdentity(t *testing.T) {
	t.Parallel()

	for content, cases := range classicIdentityCases() {
		engine := kyvernopolicy.NewEngine(
			[]kyvernov1.PolicyInterface{policy(t, content)}, nil,
		)

		for _, testCase := range cases {
			t.Run(decodeYAMLName(t, content)+"/"+testCase.name, func(t *testing.T) {
				t.Parallel()

				violations, err := engine.EvaluateAs(t.Context(), testCase.doc, testCase.identity)
				require.NoError(t, err)
				assertIdentityCase(t, testCase, violations)
			})
		}
	}
}

func decodeYAMLName(t *testing.T, content string) string {
	t.Helper()

	metadata, _ := decodeYAML(t, content)["metadata"].(map[string]any)
	name, _ := metadata["name"].(string)

	return name
}

func TestEvaluate_IsEvaluateAsWithoutIdentity(t *testing.T) {
	t.Parallel()

	engine := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{policy(t, onlyFluxPolicy)}, nil,
	)

	plain, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)

	undeclared, err := engine.EvaluateAs(t.Context(), configMap("default", nil), nil)
	require.NoError(t, err)

	require.Len(t, plain, 1)
	assert.Equal(t, undeclared, plain)

	cel := celEngine(t, nil, validatingPolicy(t, sprintf(
		onlyFluxCELPolicy,
		"request.userInfo.username == '"+fluxController+"'",
	)))

	celPlain, err := cel.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)

	celUndeclared, err := cel.EvaluateAs(t.Context(), configMap("default", nil), nil)
	require.NoError(t, err)

	require.Len(t, celPlain, 1)
	assert.Equal(t, celUndeclared, celPlain)
}

// onlyFluxCELPolicy denies a ConfigMap unless Flux's kustomize-controller
// applies it. %s is the validation expression.
const onlyFluxCELPolicy = `
apiVersion: policies.kyverno.io/v1
kind: ValidatingPolicy
metadata:
  name: only-flux
spec:
  validationActions: [Deny]
  matchConstraints:
    resourceRules:
    - apiGroups: [""]
      apiVersions: ["v1"]
      operations: ["CREATE"]
      resources: ["configmaps"]
  validations:
  - expression: "%s"
    message: "only Flux may apply ConfigMaps"
`

func TestCELEvaluateAs_PolicyReadingIdentity(t *testing.T) {
	t.Parallel()

	unlabelled := configMap("default", nil)

	for expression, cases := range map[string][]identityCase{
		"request.userInfo.username == '" + fluxController + "'": {
			{name: "flux is admitted", doc: unlabelled, identity: fluxIdentity()},
			{name: "tenant is rejected", doc: unlabelled, identity: tenantIdentity(), blocking: 1},
			{name: "no identity is an empty requester", doc: unlabelled, errored: 1},
			{name: "unselected document passes", doc: secret("default"), identity: tenantIdentity()},
		},
		"'system:serviceaccounts:flux-system' in request.userInfo.groups": {
			{name: "flux group is admitted", doc: unlabelled, identity: fluxIdentity()},
			{name: "tenant group is rejected", doc: unlabelled, identity: tenantIdentity(), blocking: 1},
		},
	} {
		engine := celEngine(t, nil, validatingPolicy(t, sprintf(onlyFluxCELPolicy, expression)))

		for _, testCase := range cases {
			t.Run(expression+"/"+testCase.name, func(t *testing.T) {
				t.Parallel()

				violations, err := engine.EvaluateAs(t.Context(), testCase.doc, testCase.identity)
				require.NoError(t, err)
				assertIdentityCase(t, testCase, violations)
			})
		}
	}
}
