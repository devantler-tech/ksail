# Partial namespace context for offline policy checks

Status: Proposed for #7258.

Offline Kyverno checks previously treated every namespace selector as unknown when its Namespace
was absent from rendered manifests. This includes namespaces created by Kubernetes or an operator.
Repeated rendering of the same resources then repeated the same warning, hiding useful diagnostics.

Kubernetes guarantees that every Namespace has the immutable
[`kubernetes.io/metadata.name` label](https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/#automatic-labelling),
whose value is its name. KSail can use this fact without connecting to a cluster or assuming which
labels an installer, operator, or user might add.

For an unrendered Namespace, both classic Kyverno and CEL ValidatingPolicy checks evaluate a
namespace selector only when that label determines its result. Selector requirements are conjunctive:
a false requirement on the namespace name decides the whole selector, even when other requirements
are unknown. A true name requirement does not establish the truth of any custom-label requirement.
Unknown custom labels are never treated as absent, including for `NotIn` and `DoesNotExist`.

Classic policies require every selector in the rule's match, exclude, and failure-action override
context to be decidable before receiving partial labels. Other rules remain independently evaluable,
subject to the existing `applyRules: One` ordering constraint. CEL expressions reading
`namespaceObject` still need a rendered Namespace; a name-only matching context does not establish
the contents of that object. Rendered and shared Namespace precedence is unchanged.

Warnings caused by unknown namespace context are grouped by policy name, rule, namespace and reason
across the validation run. Each group retains the number of evaluations, distinct resource identities
and distinct sources, plus a deterministic representative diagnostic with resource/source attribution.
Resource counts describe identities, not identical content across overlays. Different reasons and
rules remain separate, and audit failures, blocking failures and unrelated offline limitations retain
their individual diagnostics. Grouping does not make an unknown evaluation pass.

This choice deliberately avoids predicting Flux or other operators' labels. Their versions and
configuration can differ from the manifests being validated. The check remains opt-in, and its
warnings continue to describe the limits of offline evaluation.
