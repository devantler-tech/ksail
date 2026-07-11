package v1alpha1

// OIDCCAContainerPath is the well-known path where the OIDC CA certificate
// is mounted inside Kubernetes node containers and pods. All distributions
// use this path for --oidc-ca-file when caFile is configured, and each
// distribution mounts/embeds the host CA file at this location.
const OIDCCAContainerPath = "/etc/kubernetes/pki/oidc-ca.crt"

// OIDCSpec defines OIDC authentication configuration for a KSail cluster.
// When IssuerURL is set, KSail configures the Kubernetes API server with OIDC
// flags and sets up kubeconfig with exec-based OIDC credentials.
type OIDCSpec struct {
	// IssuerURL is the OIDC provider's issuer URL (e.g., https://dex.example.com).
	// Setting this field enables OIDC authentication for the cluster.
	IssuerURL string `json:"issuerURL,omitzero" jsonschema_description:"OIDC provider issuer URL"` //nolint:lll,tagliatelle // issuerURL is the standard OIDC convention
	// ClientID is the OIDC client ID for kubectl authentication.
	ClientID string `json:"clientID,omitzero" jsonschema_description:"OIDC client ID for kubectl"` //nolint:lll,tagliatelle // clientID is the standard OIDC convention
	// ExtraScopes are additional OIDC scopes to request beyond the default 'openid' scope.
	ExtraScopes []string `json:"extraScopes,omitzero" jsonschema_description:"Additional OIDC scopes beyond openid"` //nolint:lll
	// UsernameClaim is the JWT claim to use as the Kubernetes username.
	UsernameClaim string `default:"email" json:"usernameClaim,omitzero" jsonschema_description:"JWT claim for username"` //nolint:lll
	// UsernamePrefix is prepended to usernames from the OIDC provider.
	UsernamePrefix string `default:"oidc:" json:"usernamePrefix,omitzero" jsonschema_description:"Prefix for OIDC usernames"` //nolint:lll
	// GroupsClaim is the JWT claim to use for Kubernetes group membership.
	GroupsClaim string `default:"groups" json:"groupsClaim,omitzero" jsonschema_description:"JWT claim for groups"` //nolint:lll
	// GroupsPrefix is prepended to group names from the OIDC provider.
	GroupsPrefix string `default:"oidc:" json:"groupsPrefix,omitzero" jsonschema_description:"Prefix for OIDC groups"` //nolint:lll
	// CAFile is the path to the OIDC provider's CA certificate for self-signed TLS.
	CAFile string `json:"caFile,omitzero" jsonschema_description:"CA cert path for self-signed OIDC"`
}

// Enabled returns true when OIDC authentication is configured (IssuerURL is set).
func (o *OIDCSpec) Enabled() bool {
	return o.IssuerURL != ""
}
