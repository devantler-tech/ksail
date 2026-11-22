package kyvernopolicy_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/gitops/kyvernopolicy"
	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	fluxController  = "system:serviceaccount:flux-system:kustomize-controller"
	tenantAccount   = "system:serviceaccount:tenant-a:tenant-a"
	missingIdentity = "no admission identity is declared for this document"
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

// rolesPolicy selects on a Kyverno role, which comes from cluster RBAC.
const rolesPolicy = `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: by-role
spec:
  validationFailureAction: Enforce
  rules:
  - name: check-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
        clusterRoles: ["tenant-admin"]
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
	unsupported int
	message     string
}

func assertIdentityCase(t *testing.T, testCase identityCase, violations []kyvernopolicy.Violation) {
	t.Helper()

	blocking, unsupported := 0, 0

	for _, violation := range violations {
		if violation.Blocking {
			blocking++
		}

		if violation.Unsupported {
			unsupported++

			assert.False(t, violation.Blocking, "an Unsupported report never blocks")
			assert.Empty(
				t,
				violation.UnknownNamespace,
				"a missing identity is not a namespace limitation",
			)
			assert.Contains(t, violation.Message, testCase.message)
		}
	}

	assert.Len(t, violations, blocking+unsupported, "%+v", violations)
	assert.Equal(t, testCase.blocking, blocking, "%+v", violations)
	assert.Equal(t, testCase.unsupported, unsupported, "%+v", violations)
}

func classicIdentityCases() map[string][]identityCase {
	unlabelled := configMap("default", nil)

	return map[string][]identityCase{
		onlyFluxPolicy: {
			{name: "flux is admitted", doc: unlabelled, identity: fluxIdentity()},
			{name: "tenant is rejected", doc: unlabelled, identity: tenantIdentity(), blocking: 1},
			{
				name: "undeclared identity is named", doc: unlabelled,
				unsupported: 1, message: missingIdentity,
			},
			{name: "unselected document gets no diagnostic", doc: secret("default")},
		},
		fluxExemptPolicy: {
			{name: "excluded subject is admitted", doc: unlabelled, identity: fluxIdentity()},
			{
				name:     "other subject is rejected",
				doc:      unlabelled,
				identity: tenantIdentity(),
				blocking: 1,
			},
			{
				name: "undeclared identity is named", doc: unlabelled,
				unsupported: 1, message: missingIdentity,
			},
			{name: "unselected document gets no diagnostic", doc: secret("default")},
		},
		tenantOnlyPolicy: {
			{name: "unmatched subject is admitted", doc: unlabelled, identity: fluxIdentity()},
			{
				name:     "matched subject is rejected",
				doc:      unlabelled,
				identity: tenantIdentity(),
				blocking: 1,
			},
			{
				name: "subject-only match still names the missing identity", doc: unlabelled,
				unsupported: 1, message: missingIdentity,
			},
		},
		rolesPolicy: {
			{
				name: "roles stay unknown under a declared identity", doc: unlabelled,
				identity: tenantIdentity(), unsupported: 1, message: "unknown offline",
			},
			{
				name: "undeclared identity is named", doc: unlabelled,
				unsupported: 1, message: missingIdentity,
			},
			{name: "unselected document gets no diagnostic", doc: secret("default")},
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

	assert.Equal(t, undeclared, plain)
}

// A rule that reads the requester only alongside one that does not must not
// hide the other rule's result.
func TestEvaluateAs_ClassicIdentityRuleKeepsOtherRules(t *testing.T) {
	t.Parallel()

	mixed := policy(t, `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: mixed
spec:
  validationFailureAction: Enforce
  rules:
  - name: who
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    validate:
      message: "only Flux"
      deny:
        conditions:
          any:
          - key: "{{ serviceAccountNamespace }}"
            operator: NotEquals
            value: flux-system
  - name: check-team
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    validate:
      message: "label team is required"
      pattern:
        metadata:
          labels:
            team: "?*"
`)
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{mixed}, nil)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 2, "%+v", violations)
	assert.Equal(t, "who", violations[0].Rule)
	assert.True(t, violations[0].Unsupported)
	assert.Equal(t, "check-team", violations[1].Rule)
	assert.True(t, violations[1].Blocking)
}

// A field named like an identity variable, selected from the document, is not
// the requester.
func TestEvaluateAs_ClassicDocumentFieldIsNotIdentity(t *testing.T) {
	t.Parallel()

	podAccount := policy(t, `
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: pod-account
spec:
  validationFailureAction: Enforce
  rules:
  - name: no-default-account
    match:
      any:
      - resources:
          kinds: ["ConfigMap"]
    validate:
      message: "uses {{ request.object.spec.serviceAccountName || 'none' }}"
      deny:
        conditions:
          any:
          - key: "{{ request.object.metadata.name }}"
            operator: Equals
            value: settings
`)
	engine := kyvernopolicy.NewEngine([]kyvernov1.PolicyInterface{podAccount}, nil)

	violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
	require.NoError(t, err)
	require.Len(t, violations, 1, "%+v", violations)
	assert.True(t, violations[0].Blocking)
	assert.False(t, violations[0].Unsupported)
}

func TestEvaluateAs_PolicyNotReadingIdentityIgnoresIt(t *testing.T) {
	t.Parallel()

	classic := kyvernopolicy.NewEngine(
		[]kyvernov1.PolicyInterface{requireTeam(t, "Enforce")}, nil,
	)
	cel := celEngine(t, nil, requireTeamCEL(t, "v1", "Deny"))

	for _, identity := range []*kyvernopolicy.Identity{fluxIdentity(), tenantIdentity()} {
		classicUndeclared, err := classic.EvaluateAs(t.Context(), configMap("default", nil), nil)
		require.NoError(t, err)

		classicDeclared, err := classic.EvaluateAs(t.Context(), configMap("default", nil), identity)
		require.NoError(t, err)

		require.Len(t, classicUndeclared, 1)
		assert.True(t, classicUndeclared[0].Blocking)
		assert.Equal(t, classicUndeclared, classicDeclared)

		celUndeclared, err := cel.EvaluateAs(t.Context(), configMap("default", nil), nil)
		require.NoError(t, err)

		celDeclared, err := cel.EvaluateAs(t.Context(), configMap("default", nil), identity)
		require.NoError(t, err)

		require.Len(t, celUndeclared, 1)
		assert.True(t, celUndeclared[0].Blocking)
		assert.Equal(t, celUndeclared, celDeclared)
	}
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

	engine := celEngine(t, nil, validatingPolicy(t, sprintf(
		onlyFluxCELPolicy,
		"request.userInfo.username == '"+fluxController+"'",
	)))
	unlabelled := configMap("default", nil)

	for _, testCase := range []identityCase{
		{name: "flux is admitted", doc: unlabelled, identity: fluxIdentity()},
		{name: "tenant is rejected", doc: unlabelled, identity: tenantIdentity(), blocking: 1},
		{
			name: "undeclared identity is named", doc: unlabelled,
			unsupported: 1, message: missingIdentity,
		},
		{name: "unselected document gets no diagnostic", doc: secret("default")},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			violations, err := engine.EvaluateAs(t.Context(), testCase.doc, testCase.identity)
			require.NoError(t, err)
			assertIdentityCase(t, testCase, violations)
		})
	}
}

func TestCELEvaluateAs_IdentityDetection(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		expression string
		reads      bool
	}{
		{expression: "request.userInfo.username != ''", reads: true},
		{expression: ".request.userInfo.username != ''", reads: true},
		{expression: "has(request.userInfo.username)", reads: true},
		{expression: "request.?userInfo.username.orValue('') != ''", reads: true},
		{expression: "request.userInfo.groups.exists(g, g == 'system:masters')", reads: true},
		{
			expression: "[object.metadata.name].all(request, .request.userInfo.username != '')",
			reads:      true,
		},
		{expression: "[object.metadata.name].all(request, request != '')", reads: false},
		{expression: "[object.metadata.name].all(request, request != 'userInfo')", reads: false},
		{expression: "request.operation == 'CREATE'", reads: false},
		{expression: "!has(object.metadata.userInfo)", reads: false},
	} {
		t.Run(testCase.expression, func(t *testing.T) {
			t.Parallel()

			engine := celEngine(t, nil, validatingPolicy(t, sprintf(
				onlyFluxCELPolicy, testCase.expression,
			)))

			violations, err := engine.Evaluate(t.Context(), configMap("default", nil))
			require.NoError(t, err)

			if testCase.reads {
				require.Len(t, violations, 1, "%+v", violations)
				assert.True(t, violations[0].Unsupported)
				assert.Contains(t, violations[0].Message, missingIdentity)

				return
			}

			for _, violation := range violations {
				assert.False(t, violation.Unsupported, "%+v", violation)
			}
		})
	}
}
