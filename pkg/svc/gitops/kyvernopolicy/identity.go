package kyvernopolicy

import (
	"slices"

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
// has none, which Kyverno's matcher treats as an empty requester.
func (identity *Identity) requestInfo() kyvernov2.RequestInfo {
	return kyvernov2.RequestInfo{AdmissionUserInfo: identity.userInfo()}
}
