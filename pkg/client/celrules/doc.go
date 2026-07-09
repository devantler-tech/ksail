// Package celrules provides a client for validating rendered Kubernetes
// manifests against user-supplied CEL (Common Expression Language) rules.
//
// It is the policy-rule dimension of shift-left validation: each rendered
// document is bound as the CEL variable "object", and every rule expression
// is evaluated against it. An expression that evaluates to true passes; false
// (or a runtime evaluation error) is reported as a violation, attributed to
// the offending resource. Rules carry a severity so callers can distinguish a
// hard failure (error) from an advisory (warning/info).
//
// The engine is deliberately decoupled from the render pipeline and the CLI:
// it operates on raw manifest bytes so it can be unit-tested in isolation and
// wired into "workload validate" as a separate increment.
package celrules
