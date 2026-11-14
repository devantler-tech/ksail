package gen

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"github.com/fluxcd/pkg/apis/kustomize"
)

const (
	healthCheckFieldCurrent    = "current"
	healthCheckFieldInProgress = "inProgress"
	healthCheckFieldFailed     = "failed"
)

var (
	errInvalidHealthCheckExpr    = errors.New("invalid health-check-expr")
	errNotAPIVersion             = errors.New("not an API version such as v1 or cert-manager.io/v1")
	errUnknownHealthCheckField   = errors.New("unknown field")
	errDuplicateHealthCheckField = errors.New("is set more than once for this resource")

	// apiVersionPattern matches a Kubernetes API version: an optional DNS-style group
	// followed by a version such as v1, v1beta1 or v2alpha3.
	apiVersionPattern = regexp.MustCompile(
		`^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?v[0-9]+((alpha|beta)[0-9]+)?$`,
	)
)

// healthCheckTarget identifies the custom resources one health check applies to.
type healthCheckTarget struct {
	apiVersion string
	kind       string
}

// parseHealthCheckExprs turns repeated --health-check-expr values into the
// HelmRelease's spec.healthCheckExprs.
//
// Each value is "<apiVersion>[/<Kind>][:<field>]=<CEL>", where field is current
// (the default), inProgress or failed. Values naming the same apiVersion and kind
// fill one entry, in the order the targets first appear. Omitting the kind applies
// the entry to every kind in the API group, as Flux documents. Every entry needs a
// current expression, which the CRD requires.
func parseHealthCheckExprs(values []string) ([]kustomize.CustomHealthCheck, error) {
	if len(values) == 0 {
		return nil, nil
	}

	order := []healthCheckTarget{}
	checks := map[healthCheckTarget]*kustomize.CustomHealthCheck{}

	for _, value := range values {
		target, field, expression, err := parseHealthCheckExpr(value)
		if err != nil {
			return nil, err
		}

		check, seen := checks[target]
		if !seen {
			check = &kustomize.CustomHealthCheck{APIVersion: target.apiVersion, Kind: target.kind}
			checks[target] = check

			order = append(order, target)
		}

		err = setHealthCheckField(check, field, expression)
		if err != nil {
			return nil, fmt.Errorf("%w %q: %w", errInvalidHealthCheckExpr, value, err)
		}
	}

	result := make([]kustomize.CustomHealthCheck, 0, len(order))

	for _, target := range order {
		check := checks[target]
		if check.Current == "" {
			return nil, fmt.Errorf(
				"%w: %s has no current expression; add %s:current=<CEL>",
				errInvalidHealthCheckExpr,
				target,
				target,
			)
		}

		result = append(result, *check)
	}

	return result, nil
}

// parseHealthCheckExpr splits one "<apiVersion>[/<Kind>][:<field>]=<CEL>" value.
func parseHealthCheckExpr(value string) (healthCheckTarget, string, string, error) {
	selector, expression, found := strings.Cut(value, "=")
	expression = strings.TrimSpace(expression)

	if !found || expression == "" {
		return healthCheckTarget{}, "", "", fmt.Errorf(
			"%w %q: expected <apiVersion>[/<Kind>][:current|inProgress|failed]=<CEL>",
			errInvalidHealthCheckExpr,
			value,
		)
	}

	targetText, field, hasField := strings.Cut(strings.TrimSpace(selector), ":")
	if !hasField {
		field = healthCheckFieldCurrent
	}

	target, err := parseHealthCheckTarget(targetText)
	if err != nil {
		return healthCheckTarget{}, "", "", fmt.Errorf(
			"%w %q: %w",
			errInvalidHealthCheckExpr,
			value,
			err,
		)
	}

	return target, field, expression, nil
}

// parseHealthCheckTarget reads "<apiVersion>[/<Kind>]". A last path segment that
// starts with an upper-case letter is the kind, so "v1/ConfigMap" names a core kind
// and "bitnami.com/v1alpha1" names every kind in that group.
func parseHealthCheckTarget(text string) (healthCheckTarget, error) {
	apiVersion := text
	kind := ""

	if before, last, found := strings.CutLast(text, "/"); found && last != "" &&
		unicode.IsUpper([]rune(last)[0]) {
		apiVersion = before
		kind = last
	}

	if !apiVersionPattern.MatchString(apiVersion) {
		return healthCheckTarget{}, fmt.Errorf("%q is %w", apiVersion, errNotAPIVersion)
	}

	return healthCheckTarget{apiVersion: apiVersion, kind: kind}, nil
}

func setHealthCheckField(check *kustomize.CustomHealthCheck, field, expression string) error {
	var slot *string

	switch field {
	case healthCheckFieldCurrent:
		slot = &check.Current
	case healthCheckFieldInProgress:
		slot = &check.InProgress
	case healthCheckFieldFailed:
		slot = &check.Failed
	default:
		return fmt.Errorf(
			"%w %q, expected %s, %s or %s",
			errUnknownHealthCheckField,
			field,
			healthCheckFieldCurrent,
			healthCheckFieldInProgress,
			healthCheckFieldFailed,
		)
	}

	if *slot != "" {
		return fmt.Errorf("%s %w", field, errDuplicateHealthCheckField)
	}

	*slot = expression

	return nil
}

func (t healthCheckTarget) String() string {
	if t.kind == "" {
		return t.apiVersion
	}

	return t.apiVersion + "/" + t.kind
}

// setHealthCheckExprs sets spec.healthCheckExprs from the parsed flag values.
func setHealthCheckExprs(helmRelease *helmv2.HelmRelease, values []string) error {
	checks, err := parseHealthCheckExprs(values)
	if err != nil {
		return err
	}

	helmRelease.Spec.HealthCheckExprs = checks

	return nil
}
