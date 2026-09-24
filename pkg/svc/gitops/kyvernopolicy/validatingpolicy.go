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

// httpReference matches the CEL http library identifier. A policy whose
// expressions mention it is never evaluated: the library makes real requests
// for any URL without an exact mock, and validation must not reach the
// network. The match is deliberately broad; a false positive only turns a
// policy into a warning.
var httpReference = regexp.MustCompile(`(^|[^A-Za-z0-9_.])http([^A-Za-z0-9_]|$)`)

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
}

// CELEngine evaluates a fixed set of CEL ValidatingPolicies against documents.
type CELEngine struct {
	policies   []compiledCELPolicy
	namespaces map[string]*corev1.Namespace
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

	known := renderedNamespaces(namespaces)
	resolve := func(name string) *corev1.Namespace { return known[name] }

	compiler := vpolcompiler.NewCompiler()
	compiled := make([]compiledCELPolicy, 0, len(policies))

	for _, policy := range policies {
		spec := policy.GetValidatingPolicySpec()
		if !spec.AdmissionEnabled() ||
			spec.EvaluationMode() == policieskyvernoio.EvaluationModeJSON {
			continue
		}

		usesHTTP, err := referencesHTTP(spec)
		if err != nil {
			return nil, fmt.Errorf("inspect %s: %w", celPolicyName(policy), err)
		}

		if usesHTTP {
			compiled = append(compiled, compiledCELPolicy{
				policy: policy,
				unsupported: "the policy uses the CEL http library, which makes network " +
					"requests, so it is not evaluated offline",
			})

			continue
		}

		provider, err := vpolengine.NewProvider(
			compiler,
			[]policiesv1beta1.ValidatingPolicyLike{policy},
			nil,
		)
		if err != nil {
			return nil, fmt.Errorf("compile %s: %w", celPolicyName(policy), err)
		}

		compiled = append(compiled, compiledCELPolicy{policy: policy})
		last := &compiled[len(compiled)-1]
		last.engine = vpolengine.NewEngine(provider, resolve, matching.NewMatcher())
	}

	return &CELEngine{policies: compiled, namespaces: known}, nil
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
			TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Namespace"},
			ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
		}
	}

	return known
}

// Evaluate applies every policy to doc, simulating its creation, and returns
// the failing and erroring validations. A NamespacedValidatingPolicy applies
// only to documents in its own namespace. A policy that selects on namespace
// labels, for a document whose Namespace is not among the rendered documents,
// is reported as Unsupported instead of being matched against labels it cannot
// see. As with Evaluate, doc's namespace is used as given.
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

		if entry.unsupported != "" {
			violations = append(violations, Violation{
				Policy:      celPolicyName(policy),
				Message:     entry.unsupported,
				Unsupported: true,
			})

			continue
		}

		_, known := e.namespaces[namespace]
		if namespace != "" && !known && selectsOnNamespaceLabels(policy) {
			violations = append(violations, Violation{
				Policy: celPolicyName(policy),
				Message: fmt.Sprintf(
					"namespace %q is not among the rendered documents, so its labels are "+
						"unknown and this policy's namespaceSelector cannot be evaluated offline",
					namespace,
				),
				Unsupported: true,
			})

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

// referencesHTTP reports whether any expression in spec mentions the CEL http
// library.
func referencesHTTP(spec *policiesv1beta1.ValidatingPolicySpec) (bool, error) {
	encoded, err := json.Marshal(spec)
	if err != nil {
		return false, fmt.Errorf("encode policy spec: %w", err)
	}

	var strs []string

	var decoded any

	err = json.Unmarshal(encoded, &decoded)
	if err != nil {
		return false, fmt.Errorf("decode policy spec: %w", err)
	}

	collectStrings(decoded, &strs)

	if slices.ContainsFunc(strs, httpReference.MatchString) {
		return true, nil
	}

	return false, nil
}

func collectStrings(value any, out *[]string) {
	switch typed := value.(type) {
	case string:
		*out = append(*out, typed)
	case map[string]any:
		for _, v := range typed {
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
