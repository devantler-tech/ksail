package kyvernopolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	"cel.dev/cel-go/cel"
	celast "cel.dev/cel-go/common/ast"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	policieskyvernoio "github.com/kyverno/api/api/policies.kyverno.io"
	policiesv1beta1 "github.com/kyverno/api/api/policies.kyverno.io/v1beta1"
	celengine "github.com/kyverno/kyverno/pkg/cel/engine"
	"github.com/kyverno/kyverno/pkg/cel/libs"
	"github.com/kyverno/kyverno/pkg/cel/matching"
	vpolcompiler "github.com/kyverno/kyverno/pkg/cel/policies/vpol/compiler"
	vpolengine "github.com/kyverno/kyverno/pkg/cel/policies/vpol/engine"
	engineapi "github.com/kyverno/kyverno/pkg/engine/api"
	admissionv1 "k8s.io/api/admission/v1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
)

const (
	celPolicyGroup                 = "policies.kyverno.io"
	kindValidatingPolicy           = "ValidatingPolicy"
	kindNamespacedValidatingPolicy = "NamespacedValidatingPolicy"
)

// isCELPolicyVersion reports whether version is one a policies.kyverno.io
// ValidatingPolicy is served under. They share one spec type, so each decodes
// into v1beta1.
func isCELPolicyVersion(version string) bool {
	switch version {
	case "v1", "v1beta1", "v1alpha1":
		return true
	default:
		return false
	}
}

// errOffline is what every cluster, registry or global-context lookup returns
// while evaluating offline. Its text is how a rule error caused by a lookup is
// told apart from one the policy itself raises.
var errOffline = errors.New(
	"ksail evaluates Kyverno policies offline and cannot perform this lookup",
)

// httpLibrary is the CEL variable through which Kyverno's http library is
// called (`http.Get(…)`, `http.Post(…)`, `http.Client(…)`). A policy whose
// expressions refer to it is never evaluated: the library makes real requests
// for any URL without an exact mock, and validation must not reach the network.
const httpLibrary = "http"

// namespaceObjectVariable is the CEL variable holding the document's
// Namespace. Offline it is only known for namespaces among the rendered
// documents.
const namespaceObjectVariable = "namespaceObject"

// httpReference and namespaceObjectReference match those identifiers as words
// in a string that does not parse as CEL here, preceded by a dot or not: a
// leading dot (`.http`) names the same root-scope variable. Kyverno may parse
// the string with options this package does not share, so the match is
// deliberately broad and also takes a field of that name: a false positive only
// turns a policy into a warning, while a miss would let an http call run.
var (
	httpReference            = regexp.MustCompile(`(^|[^A-Za-z0-9_])http([^A-Za-z0-9_]|$)`)
	namespaceObjectReference = regexp.MustCompile(
		`(^|[^A-Za-z0-9_])namespaceObject([^A-Za-z0-9_]|$)`,
	)
)

// newIdentifierParser returns a CEL environment that parses expressions as
// Kyverno policies write them, optional syntax and escaped identifiers
// included. It is only used to parse, never to type-check or evaluate.
func newIdentifierParser() (*cel.Env, error) {
	env, err := cel.NewEnv(cel.OptionalTypes(), cel.EnableIdentifierEscapeSyntax())
	if err != nil {
		return nil, fmt.Errorf("build CEL parser: %w", err)
	}

	return env, nil
}

// referencesIdentifier reports whether any string in strs refers to the CEL
// identifier name. A string that parses as CEL refers to it only when its
// syntax tree holds that identifier, written plainly or with the leading dot
// that names the root scope (`.http`), so a field of the same name selected
// from another value (`object.?spec.?http`), a map key or a string literal does
// not count. A string that does not parse is matched against fallback instead,
// so an expression that may use the identifier is never taken not to.
func referencesIdentifier(
	parser *cel.Env,
	strs []string,
	name string,
	fallback *regexp.Regexp,
) bool {
	for _, str := range strs {
		parsed, issues := parser.Parse(str)
		if issues != nil && issues.Err() != nil {
			if fallback.MatchString(str) {
				return true
			}

			continue
		}

		references := celast.MatchDescendants(
			celast.NavigateAST(parsed.NativeRep()),
			func(expr celast.NavigableExpr) bool {
				return expr.Kind() == celast.IdentKind &&
					strings.TrimPrefix(expr.AsIdent(), ".") == name
			},
		)
		if len(references) > 0 {
			return true
		}
	}

	return false
}

// installOffline makes Kyverno's process-wide CEL library context the offline
// one. The vpol compiler binds its resource, image-data and global-context
// libraries to that context when it compiles a policy, and its default fake
// silently answers a missing global-context entry with null.
var installOffline = sync.OnceFunc(func() { libs.LibraryContext = offlineContext{} })

// IsValidatingPolicy reports whether doc is a CEL-based ValidatingPolicy or
// NamespacedValidatingPolicy this package evaluates.
func IsValidatingPolicy(doc map[string]any) bool {
	apiVersion, _ := doc["apiVersion"].(string)
	kind, _ := doc["kind"].(string)

	group, version, found := strings.Cut(apiVersion, "/")
	if !found || group != celPolicyGroup {
		return false
	}

	if !isCELPolicyVersion(version) {
		return false
	}

	return kind == kindValidatingPolicy || kind == kindNamespacedValidatingPolicy
}

// DecodeValidatingPolicy converts a document for which IsValidatingPolicy is
// true into a typed policy. A document that does not decode is an error.
func DecodeValidatingPolicy(doc map[string]any) (policiesv1beta1.ValidatingPolicyLike, error) {
	if !IsValidatingPolicy(doc) {
		return nil, fmt.Errorf("%w: not a policies.kyverno.io ValidatingPolicy", errDecodePolicy)
	}

	var policy policiesv1beta1.ValidatingPolicyLike

	if doc["kind"] == kindValidatingPolicy {
		policy = &policiesv1beta1.ValidatingPolicy{}
	} else {
		policy = &policiesv1beta1.NamespacedValidatingPolicy{}
	}

	err := decodeInto(doc, policy)
	if err != nil {
		return nil, err
	}

	return policy, nil
}

// compiledCELPolicy is one policy ready to evaluate, or the reason it cannot be.
type compiledCELPolicy struct {
	policy      policiesv1beta1.ValidatingPolicyLike
	engine      vpolengine.Engine
	unsupported string
	// readsNamespace is set when an expression mentions namespaceObject.
	readsNamespace bool
}

// CELEngine evaluates a fixed set of CEL ValidatingPolicies against documents.
type CELEngine struct {
	policies   []compiledCELPolicy
	namespaces map[string]*corev1.Namespace
	matcher    matching.Matcher
}

// NewCELEngine compiles the given policies. As at admission, it keeps only
// policies with admission processing enabled that evaluate Kubernetes
// resources. A policy that uses the http library is kept but reported as not
// evaluable offline. A policy that does not compile is an error: a cluster
// would not enforce it, and skipping it would report a clean run for a policy
// that was never applied. namespaces are the source's rendered documents; the
// Namespace documents among them are what namespaceSelector matches against.
func NewCELEngine(
	policies []policiesv1beta1.ValidatingPolicyLike,
	namespaces []map[string]any,
) (*CELEngine, error) {
	installOffline()
	silenceControllerRuntimeLogger()

	known := renderedNamespaces(namespaces)
	resolve := func(name string) *corev1.Namespace {
		if namespace, found := known[name]; found {
			return namespace
		}

		return namespaceForMatching(name)
	}

	parser, err := newIdentifierParser()
	if err != nil {
		return nil, err
	}

	matcher := matching.NewMatcher()
	policyCompiler := celPolicyCompiler{
		parser:   parser,
		compiler: vpolcompiler.NewCompiler(),
		resolve:  resolve,
		matcher:  matcher,
	}
	compiled := make([]compiledCELPolicy, 0, len(policies))

	for _, policy := range policies {
		spec := policy.GetValidatingPolicySpec()
		if !spec.AdmissionEnabled() ||
			spec.EvaluationMode() == policieskyvernoio.EvaluationModeJSON {
			continue
		}

		entry, err := policyCompiler.compile(policy)
		if err != nil {
			return nil, err
		}

		compiled = append(compiled, entry)
	}

	return &CELEngine{policies: compiled, namespaces: known, matcher: matcher}, nil
}

// celPolicyCompiler prepares one policy at a time for a CELEngine.
type celPolicyCompiler struct {
	parser   *cel.Env
	compiler vpolcompiler.Compiler
	resolve  celengine.NamespaceResolver
	matcher  matching.Matcher
}

// compile returns policy ready to evaluate, or with the reason it cannot be
// evaluated offline. A policy that uses the http library is never compiled.
func (c celPolicyCompiler) compile(
	policy policiesv1beta1.ValidatingPolicyLike,
) (compiledCELPolicy, error) {
	strs, err := specStrings(policy.GetValidatingPolicySpec())
	if err != nil {
		return compiledCELPolicy{}, fmt.Errorf("inspect %s: %w", celPolicyName(policy), err)
	}

	if referencesIdentifier(c.parser, strs, httpLibrary, httpReference) {
		return compiledCELPolicy{
			policy: policy,
			unsupported: "the policy uses the CEL http library, which makes network " +
				"requests, so it is not evaluated offline",
		}, nil
	}

	provider, err := vpolengine.NewProvider(
		c.compiler,
		[]policiesv1beta1.ValidatingPolicyLike{policy},
		nil,
	)
	if err != nil {
		return compiledCELPolicy{}, fmt.Errorf("compile %s: %w", celPolicyName(policy), err)
	}

	return compiledCELPolicy{
		policy: policy,
		engine: vpolengine.NewEngine(provider, c.resolve, c.matcher),
		readsNamespace: referencesIdentifier(
			c.parser, strs, namespaceObjectVariable, namespaceObjectReference,
		),
	}, nil
}

// renderedNamespaces indexes the Namespace documents among docs.
func renderedNamespaces(docs []map[string]any) map[string]*corev1.Namespace {
	known := map[string]*corev1.Namespace{}

	for _, doc := range docs {
		name, ok := NamespaceDocumentName(doc)
		if !ok {
			continue
		}

		labels, _, _ := unstructured.NestedStringMap(doc, "metadata", "labels")
		known[name] = &corev1.Namespace{
			APIVersion: "v1", Kind: kindNamespace,
			Name: name, Labels: labels,
		}
	}

	return known
}

// Evaluate applies every policy to doc, simulating its creation, and returns
// the failing and erroring validations. A NamespacedValidatingPolicy applies
// only to documents in its own namespace. A policy that selects on namespace
// labels, for a document whose Namespace is not among the rendered documents,
// is evaluated only when the immutable name label determines its selector.
// Other selectors and namespaceObject reads are reported as Unsupported.
// Such a report, like one for a policy that
// cannot run offline at all, is made only for documents the policy's other
// match constraints select. As with Evaluate, doc's namespace is used as given.
func (e *CELEngine) Evaluate(ctx context.Context, doc map[string]any) ([]Violation, error) {
	resource := &unstructured.Unstructured{Object: doc}
	namespace := resource.GetNamespace()

	var violations []Violation

	for _, entry := range e.policies {
		policy := entry.policy

		if policyNamespace := policy.GetNamespace(); policyNamespace != "" &&
			policyNamespace != namespace {
			continue
		}

		reason := e.offlineLimitation(entry, namespace, resource.GroupVersionKind().GroupKind())
		if reason != "" {
			// A limitation is only worth reporting for a document the policy
			// would select at all.
			if e.appliesIgnoringNamespace(policy, resource) {
				unknownNamespace := ""
				if entry.unsupported == "" {
					unknownNamespace = namespace
				}

				violations = append(violations, Violation{
					Policy:           celPolicyName(policy),
					Message:          reason,
					Unsupported:      true,
					UnknownNamespace: unknownNamespace,
				})
			}

			continue
		}

		response, err := entry.engine.Handle(ctx, admissionRequest(resource), nil)
		if err != nil {
			return nil, fmt.Errorf("evaluate %s: %w", celPolicyName(policy), err)
		}

		for _, result := range response.Policies {
			violations = append(violations, collectCEL(policy, result.Rules)...)
		}
	}

	return violations, nil
}

// admissionRequest builds the create request the policy would see at admission.
// Without API discovery the resource name is guessed from the kind, as kubectl
// does offline; resourceRules name plural resources, so the guess is what they
// match against.
func admissionRequest(resource *unstructured.Unstructured) celengine.EngineRequest {
	gvk := resource.GroupVersionKind()
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)

	request := celengine.Request(
		offlineContext{},
		gvk,
		gvr,
		"",
		resource.GetName(),
		resource.GetNamespace(),
		admissionv1.Create,
		authenticationv1.UserInfo{},
		resource,
		nil,
		false,
		nil,
	)

	return request
}

// collectCEL converts one policy's rule responses into violations. A failure
// blocks when the policy's validation actions include Deny. An error blocks,
// as at admission, only when the policy denies and its failurePolicy is Fail.
// An error caused by an offline lookup is Unsupported and never blocks.
func collectCEL(
	policy policiesv1beta1.ValidatingPolicyLike,
	rules []engineapi.RuleResponse,
) []Violation {
	denies := false

	for _, action := range policy.GetValidatingPolicySpec().ValidationActions() {
		if action == admissionregistrationv1.Deny {
			denies = true
		}
	}

	failClosed := policy.GetFailurePolicy(false) == admissionregistrationv1.Fail

	var violations []Violation

	for i := range rules {
		rule := &rules[i]

		switch rule.Status() {
		case engineapi.RuleStatusFail:
			violations = append(violations, Violation{
				Policy:   celPolicyName(policy),
				Rule:     rule.Name(),
				Message:  rule.Message(),
				Blocking: denies,
			})
		case engineapi.RuleStatusError:
			if strings.Contains(rule.Message(), errOffline.Error()) {
				violations = append(violations, Violation{
					Policy:      celPolicyName(policy),
					Rule:        rule.Name(),
					Message:     rule.Message(),
					Unsupported: true,
				})

				continue
			}

			violations = append(violations, Violation{
				Policy:   celPolicyName(policy),
				Rule:     rule.Name(),
				Message:  rule.Message(),
				Error:    true,
				Blocking: denies && failClosed,
			})
		case engineapi.RuleStatusPass, engineapi.RuleStatusWarn, engineapi.RuleStatusSkip:
		}
	}

	return violations
}

// isBuiltinClusterScoped reports whether groupKind is a built-in kind that has
// no namespace. There is no API discovery offline, so any other kind that
// declares no namespace is taken to be namespaced, landing in a namespace its
// applier chooses.
func isBuiltinClusterScoped(groupKind schema.GroupKind) bool {
	return slices.Contains(builtinClusterScopedKinds()[groupKind.Group], groupKind.Kind)
}

// builtinClusterScopedKinds maps each built-in API group to its kinds that
// have no namespace.
func builtinClusterScopedKinds() map[string][]string {
	return map[string][]string{
		"":                          {kindNamespace, "Node", "PersistentVolume"},
		"rbac.authorization.k8s.io": {"ClusterRole", "ClusterRoleBinding"},
		"apiextensions.k8s.io":      {"CustomResourceDefinition"},
		"apiregistration.k8s.io":    {"APIService"},
		"admissionregistration.k8s.io": {
			"MutatingWebhookConfiguration", "ValidatingWebhookConfiguration",
			"ValidatingAdmissionPolicy", "ValidatingAdmissionPolicyBinding",
		},
		"storage.k8s.io":    {"StorageClass", "CSIDriver", "CSINode", "VolumeAttachment"},
		"scheduling.k8s.io": {"PriorityClass"},
		"networking.k8s.io": {"IngressClass"},
		"node.k8s.io":       {"RuntimeClass"},
	}
}

// offlineLimitation returns why entry cannot be evaluated offline for a
// document of groupKind in namespace, or "" when it can. A namespace that is
// unknown offline is a limitation only for a policy that selects on its labels
// or reads it as namespaceObject; either would otherwise be evaluated against a
// namespace that is not there. A namespace is unknown when it is missing from
// the rendered documents, or when a namespaced document declares none.
func (e *CELEngine) offlineLimitation(
	entry compiledCELPolicy,
	namespace string,
	groupKind schema.GroupKind,
) string {
	if entry.unsupported != "" {
		return entry.unsupported
	}

	var unknown string

	if namespace == "" {
		if isBuiltinClusterScoped(groupKind) {
			return ""
		}

		unknown = "the document declares no namespace, so the namespace it lands in"
	} else {
		if _, known := e.namespaces[namespace]; known {
			return ""
		}

		unknown = fmt.Sprintf("namespace %q is not among the rendered documents, so it", namespace)
	}

	return unknownNamespaceLimitation(entry, namespace, unknown)
}

// unknownNamespaceLimitation returns why entry cannot be evaluated offline for
// a document in a namespace whose labels are unknown, described by unknown, or
// "" when the namespace-name label alone decides the policy or the policy never
// looks at the namespace.
func unknownNamespaceLimitation(entry compiledCELPolicy, namespace, unknown string) string {
	var selector *metav1.LabelSelector
	if selectsOnNamespaceLabels(entry.policy) {
		selector = entry.policy.GetValidatingPolicySpec().MatchConstraints.NamespaceSelector
	}

	switch {
	case selector != nil && namespaceSelectorExcludes(selector, namespace):
		return ""
	case selector != nil && !namespaceSelectorKnown(selector, namespace):
		return unknown + " has unknown labels and this policy's namespaceSelector " +
			"cannot be evaluated offline"
	case entry.readsNamespace:
		return unknown + " is unknown, so the namespaceObject this policy reads " +
			"cannot be evaluated offline"
	default:
		return ""
	}
}

// appliesIgnoringNamespace reports whether policy's match constraints select
// the create request for resource once the namespaceSelector is set aside,
// since that selector is exactly what may be unknowable offline. A match error
// counts as a match, so a limitation is reported rather than hidden.
func (e *CELEngine) appliesIgnoringNamespace(
	policy policiesv1beta1.ValidatingPolicyLike,
	resource *unstructured.Unstructured,
) bool {
	constraints := policy.GetValidatingPolicySpec().MatchConstraints
	if constraints == nil {
		return false
	}

	relaxed := constraints.DeepCopy()
	relaxed.NamespaceSelector = nil

	gvk := resource.GroupVersionKind()
	gvr, _ := meta.UnsafeGuessKindToResource(gvk)
	attr := admission.NewAttributesRecord(
		resource,
		nil,
		gvk,
		resource.GetNamespace(),
		resource.GetName(),
		gvr,
		"",
		admission.Create,
		nil,
		false,
		nil,
	)

	matches, err := e.matcher.Match(&matching.MatchCriteria{Constraints: relaxed}, attr, nil)
	if err != nil {
		return true
	}

	return matches
}

// specStrings returns every string in spec that may hold a CEL expression: all
// of them except a validation's static message, which is shown as written and
// never evaluated.
func specStrings(spec *policiesv1beta1.ValidatingPolicySpec) ([]string, error) {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("encode policy spec: %w", err)
	}

	var decoded any

	err = json.Unmarshal(encoded, &decoded)
	if err != nil {
		return nil, fmt.Errorf("decode policy spec: %w", err)
	}

	var strs []string

	collectStrings(decoded, &strs)

	return strs, nil
}

func collectStrings(value any, out *[]string) {
	switch typed := value.(type) {
	case string:
		*out = append(*out, typed)
	case map[string]any:
		for key, v := range typed {
			if _, static := v.(string); static && key == "message" {
				continue
			}

			collectStrings(v, out)
		}
	case []any:
		for _, v := range typed {
			collectStrings(v, out)
		}
	}
}

// selectsOnNamespaceLabels reports whether policy's match constraints select
// on namespace labels.
func selectsOnNamespaceLabels(policy policiesv1beta1.ValidatingPolicyLike) bool {
	constraints := policy.GetValidatingPolicySpec().MatchConstraints
	if constraints == nil || constraints.NamespaceSelector == nil {
		return false
	}

	selector := constraints.NamespaceSelector

	return len(selector.MatchLabels) > 0 || len(selector.MatchExpressions) > 0
}

func celPolicyName(policy policiesv1beta1.ValidatingPolicyLike) string {
	if namespace := policy.GetNamespace(); namespace != "" {
		return namespace + "/" + policy.GetName()
	}

	return policy.GetName()
}

// offlineContext is the CEL library context for offline evaluation: every
// lookup fails with errOffline instead of reaching a cluster, a registry or a
// global-context store, or silently answering null.
type offlineContext struct{}

var _ libs.Context = offlineContext{}

func (offlineContext) GetGlobalReference(string, string) (any, error) { return nil, errOffline }

func (offlineContext) GetImageData(string, []remote.Option) (map[string]any, error) {
	return nil, errOffline
}

func (offlineContext) ListResources(
	string, string, string, map[string]string,
) (*unstructured.UnstructuredList, error) {
	return nil, errOffline
}

func (offlineContext) GetResource(
	string,
	string,
	string,
	string,
) (*unstructured.Unstructured, error) {
	return nil, errOffline
}

func (offlineContext) PostResource(
	string, string, string, map[string]any,
) (*unstructured.Unstructured, error) {
	return nil, errOffline
}

func (offlineContext) ToGVR(string, string) (*schema.GroupVersionResource, error) {
	return nil, errOffline
}

func (offlineContext) GenerateResources(string, []map[string]any) error { return errOffline }

func (offlineContext) GetHTTPMocks() map[string]any { return nil }

func (offlineContext) GetGeneratedResources() []*unstructured.Unstructured { return nil }

func (offlineContext) ClearGeneratedResources() {}

func (offlineContext) SetGenerateContext(
	string, string, string, string, string, string, string, string, bool, bool,
) {
}

func (c offlineContext) Clone() libs.Context { return c }
