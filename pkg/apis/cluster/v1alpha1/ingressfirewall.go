package v1alpha1

// IngressFirewall defines whether the Talos OS-level ingress firewall is configured.
// When Enabled, KSail generates NetworkDefaultActionConfig (ingress: block) and
// NetworkRuleConfig documents as Talos machine config patches, providing defense-in-depth
// independent of the Hetzner Cloud Firewall.
// See: https://www.talos.dev/latest/talos-guides/network/ingress-firewall/
type IngressFirewall string

const (
	// IngressFirewallEnabled enables the Talos ingress firewall configuration.
	IngressFirewallEnabled IngressFirewall = "Enabled"
	// IngressFirewallDisabled disables the Talos ingress firewall configuration.
	IngressFirewallDisabled IngressFirewall = "Disabled"
)

// ValidIngressFirewalls returns all valid IngressFirewall values.
func ValidIngressFirewalls() []IngressFirewall {
	return []IngressFirewall{IngressFirewallEnabled, IngressFirewallDisabled}
}

// Set for IngressFirewall (pflag.Value interface).
func (f *IngressFirewall) Set(value string) error {
	return setEnum(f, value, ValidIngressFirewalls(), ErrInvalidIngressFirewall)
}

// String returns the string representation of the IngressFirewall.
func (f *IngressFirewall) String() string {
	return string(*f)
}

// Type returns the type of the IngressFirewall.
func (f *IngressFirewall) Type() string {
	return "IngressFirewall"
}

// Default returns the default value for IngressFirewall (Enabled).
func (f *IngressFirewall) Default() any {
	return IngressFirewallEnabled
}

// ValidValues returns all valid IngressFirewall values as strings.
func (f *IngressFirewall) ValidValues() []string {
	return validValueStrings(ValidIngressFirewalls())
}
