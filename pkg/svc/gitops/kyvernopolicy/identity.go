package kyvernopolicy

import (
	"encoding/json"
	"regexp"
	"slices"

	kyvernov1 "github.com/kyverno/kyverno/api/kyverno/v1"
	kyvernov2 "github.com/kyverno/kyverno/api/kyverno/v2"
	authenticationv1 "k8s.io/api/authentication/v1"
)

// Identity is the requester an admission request is attributed to: whoever
// applies the document to the cluster, such as Flux's kustomize-controller
// service account. It carries only what the applier's identity declares. The
// roles and cluster roles Kyverno resolves for a requester come from cluster
// RBAC, so they are unknown offline and are never filled in.
type Identity struct {
	// Username is the requester's user name; for a service account it is
	// system:serviceaccount:<namespace>:<name>.
	Username string
	// Groups are the groups the requester belongs to.
	Groups []string
}

// userInfo returns the admission user info for identity; a nil identity has
// none.
func (identity *Identity) userInfo() authenticationv1.UserInfo {
	if identity == nil {
		return authenticationv1.UserInfo{}
	}

	return authenticationv1.UserInfo{
		Username: identity.Username,
		Groups:   slices.Clone(identity.Groups),
	}
}

// requestInfo returns the Kyverno admission info for identity; a nil identity
// has none, which Kyverno's matcher treats as "match on no user at all".
func (identity *Identity) requestInfo() kyvernov2.RequestInfo {
	return kyvernov2.RequestInfo{AdmissionUserInfo: identity.userInfo()}
}

// undeclaredIdentityReason is the shared reason a policy or rule that reads
// the requester identity is not evaluated without a declared identity.
const undeclaredIdentityReason = "no admission identity is declared for this document, " +
	"so it is not evaluated offline rather than evaluated as an empty requester"

// undeclaredIdentityViolation reports a classic rule that reads the requester
// identity while none is declared for the document.
func undeclaredIdentityViolation(policy kyvernov1.PolicyInterface, rule string) Violation {
	return Violation{
		Policy: policyName(policy),
		Rule:   rule,
		Message: "this rule reads the requester identity (match or exclude subjects, " +
			"roles or clusterRoles, or request.userInfo, request.roles, " +
			"request.clusterRoles, serviceAccountName or serviceAccountNamespace) and " +
			undeclaredIdentityReason,
		Unsupported: true,
	}
}

// unknownRolesViolation reports a classic rule that reads the requester's
// Kyverno roles or cluster roles. A declared identity carries neither: Kyverno
// resolves them from cluster RBAC, so evaluating the rule would decide it
// against roles the requester may well hold.
func unknownRolesViolation(policy kyvernov1.PolicyInterface, rule string) Violation {
	return Violation{
		Policy: policyName(policy),
		Rule:   rule,
		Message: "this rule reads the requester's roles or clusterRoles, which Kyverno " +
			"resolves from cluster RBAC and are unknown offline, so it is not evaluated " +
			"offline",
		Unsupported: true,
	}
}

// unknownRuleReachViolation reports a rule the engine is not known to reach:
// an earlier rule cannot be evaluated offline for the requester, and the
// policy sets applyRules: One, so the engine may stop before this rule runs.
func unknownRuleReachViolation(policy kyvernov1.PolicyInterface, rule string) Violation {
	return Violation{
		Policy: policyName(policy),
		Rule:   rule,
		Message: "an earlier rule reads requester details that are unknown offline and " +
			"this policy sets applyRules: One, so whether this rule is reached is unknown",
		Unsupported: true,
	}
}

// Classic rules read the requester through these JMESPath variables. Each
// must be the root of its path: a field of the same name selected from
// another value (request.object.spec.serviceAccountName) is not the requester.
var (
	identityVariableReference = regexp.MustCompile(
		`(^|[^A-Za-z0-9_.])(request\.(userInfo|roles|clusterRoles)|` +
			`serviceAccountName|serviceAccountNamespace)([^A-Za-z0-9_]|$)`,
	)
	rolesVariableReference = regexp.MustCompile(
		`(^|[^A-Za-z0-9_.])request\.(roles|clusterRoles)([^A-Za-z0-9_]|$)`,
	)
)

// ruleIdentityUse is how a classic rule depends on the requester.
type ruleIdentityUse struct {
	// identity is set when the rule depends on the requester at all.
	identity bool
	// roles is set when it depends on the requester's roles or cluster roles,
	// which a declared Identity does not carry.
	roles bool
}

// classicRuleIdentityUse reports how rule depends on the requester: through
// the subjects, roles or clusterRoles of its match or exclude filters, or
// through a string that reads the requester's JMESPath variables. A rule that
// does not encode is taken to depend on neither, which is how it was
// evaluated before identities were declared.
func classicRuleIdentityUse(rule kyvernov1.Rule) ruleIdentityUse {
	use := filterIdentityUse(rule.MatchResources)
	if rule.ExcludeResources != nil {
		excluded := filterIdentityUse(*rule.ExcludeResources)
		use.identity = use.identity || excluded.identity
		use.roles = use.roles || excluded.roles
	}

	encoded, err := json.Marshal(rule)
	if err != nil {
		return use
	}

	var decoded any

	err = json.Unmarshal(encoded, &decoded)
	if err != nil {
		return use
	}

	var strs []string

	collectValueStrings(decoded, &strs)

	for _, str := range strs {
		if identityVariableReference.MatchString(str) {
			use.identity = true
		}

		if rolesVariableReference.MatchString(str) {
			use.identity = true
			use.roles = true
		}
	}

	return use
}

// filterIdentityUse reports how the user filters of a match or exclude block,
// plain or in its any and all lists, depend on the requester.
func filterIdentityUse(block kyvernov1.MatchResources) ruleIdentityUse {
	use := userInfoIdentityUse(block.UserInfo)

	for _, filter := range slices.Concat(block.Any, block.All) {
		filterUse := userInfoIdentityUse(filter.UserInfo)
		use.identity = use.identity || filterUse.identity
		use.roles = use.roles || filterUse.roles
	}

	return use
}

func userInfoIdentityUse(info kyvernov1.UserInfo) ruleIdentityUse {
	roles := len(info.Roles) > 0 || len(info.ClusterRoles) > 0

	return ruleIdentityUse{identity: roles || len(info.Subjects) > 0, roles: roles}
}

// collectValueStrings collects every string value in value, map keys aside:
// a key names a field of the document a pattern checks, never a variable.
func collectValueStrings(value any, out *[]string) {
	switch typed := value.(type) {
	case string:
		*out = append(*out, typed)
	case map[string]any:
		for _, v := range typed {
			collectValueStrings(v, out)
		}
	case []any:
		for _, v := range typed {
			collectValueStrings(v, out)
		}
	}
}

// identityFreeRule returns a copy of rule whose match no longer depends on
// the requester: user filters are removed from its match filters, which can
// only widen the match (a filter left with nothing else matches every kind),
// and exclude filters that select on the requester are dropped, since
// excluding less can only widen it too.
func identityFreeRule(rule kyvernov1.Rule) kyvernov1.Rule {
	broadened := *rule.DeepCopy()

	broadened.MatchResources.UserInfo = kyvernov1.UserInfo{}
	if broadened.MatchResources.ResourceDescription.IsEmpty() &&
		len(broadened.MatchResources.Any) == 0 && len(broadened.MatchResources.All) == 0 {
		broadened.MatchResources.Kinds = []string{"*"}
	}

	for _, filters := range []kyvernov1.ResourceFilters{
		broadened.MatchResources.Any, broadened.MatchResources.All,
	} {
		for index := range filters {
			filters[index].UserInfo = kyvernov1.UserInfo{}
			if filters[index].ResourceDescription.IsEmpty() {
				filters[index].Kinds = []string{"*"}
			}
		}
	}

	broadened.ExcludeResources = exclusionsWithout(
		broadened.ExcludeResources,
		func(filter kyvernov1.ResourceFilter) bool {
			return userInfoIdentityUse(filter.UserInfo).identity
		},
	)

	return broadened
}
