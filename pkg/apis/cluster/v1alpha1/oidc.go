package v1alpha1

// OIDCSpec defines OIDC authentication configuration for a KSail cluster.
// When IssuerURL is set, KSail configures the Kubernetes API server with OIDC
// flags and sets up kubeconfig with exec-based OIDC credentials.
type OIDCSpec struct {
	// IssuerURL is the OIDC provider's issuer URL (e.g., https://dex.example.com).
	// Setting this field enables OIDC authentication for the cluster.
	IssuerURL string `json:"issuerURL,omitzero" jsonschema:"description=OIDC provider issuer URL (e.g. https://dex.example.com)"`
	// ClientID is the OIDC client ID for kubectl authentication.
	ClientID string `json:"clientID,omitzero" jsonschema:"description=OIDC client ID for kubectl authentication"`
	// ExtraScopes are additional OIDC scopes to request beyond the default 'openid' scope.
	ExtraScopes []string `json:"extraScopes,omitzero" jsonschema:"description=Additional OIDC scopes beyond openid (e.g. email profile groups)"`
	// UsernameClaim is the JWT claim to use as the Kubernetes username.
	UsernameClaim string `default:"email" json:"usernameClaim,omitzero" jsonschema:"description=JWT claim for Kubernetes username"`
	// UsernamePrefix is prepended to usernames from the OIDC provider.
	UsernamePrefix string `default:"oidc:" json:"usernamePrefix,omitzero" jsonschema:"description=Prefix for OIDC usernames"`
	// GroupsClaim is the JWT claim to use for Kubernetes group membership.
	GroupsClaim string `default:"groups" json:"groupsClaim,omitzero" jsonschema:"description=JWT claim for Kubernetes groups"`
	// GroupsPrefix is prepended to group names from the OIDC provider.
	GroupsPrefix string `default:"oidc:" json:"groupsPrefix,omitzero" jsonschema:"description=Prefix for OIDC groups"`
	// CAFile is the path to the OIDC provider's CA certificate for self-signed TLS.
	CAFile string `json:"caFile,omitzero" jsonschema:"description=Path to CA certificate for self-signed OIDC providers"`
}

// Enabled returns true when OIDC authentication is configured (IssuerURL is set).
func (o *OIDCSpec) Enabled() bool {
	return o.IssuerURL != ""
}
