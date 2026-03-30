package v1alpha1

import (
	"fmt"
	"strings"
)

// MetricsServer defines the Metrics Server options for a KSail cluster.
type MetricsServer string

const (
	// MetricsServerDefault relies on the distribution's default behavior for metrics server.
	MetricsServerDefault MetricsServer = "Default"
	// MetricsServerEnabled ensures Metrics Server is installed.
	MetricsServerEnabled MetricsServer = "Enabled"
	// MetricsServerDisabled ensures Metrics Server is not installed.
	MetricsServerDisabled MetricsServer = "Disabled"
)

// Set for MetricsServer (pflag.Value interface).
func (m *MetricsServer) Set(value string) error {
	for _, ms := range ValidMetricsServers() {
		if strings.EqualFold(value, string(ms)) {
			*m = ms

			return nil
		}
	}

	return fmt.Errorf(
		"%w: %s (valid options: %s, %s, %s)",
		ErrInvalidMetricsServer,
		value,
		MetricsServerDefault,
		MetricsServerEnabled,
		MetricsServerDisabled,
	)
}

// String returns the string representation of the MetricsServer.
func (m *MetricsServer) String() string {
	return string(*m)
}

// Type returns the type of the MetricsServer.
func (m *MetricsServer) Type() string {
	return "MetricsServer"
}

// Default returns the default value for MetricsServer (Default, which defers to the distribution).
func (m *MetricsServer) Default() any {
	return MetricsServerDefault
}

// ValidValues returns all valid MetricsServer values as strings.
func (m *MetricsServer) ValidValues() []string {
	return []string{
		string(MetricsServerDefault),
		string(MetricsServerEnabled),
		string(MetricsServerDisabled),
	}
}

// EffectiveValue resolves Default to its concrete meaning for the given
// distribution. Enabled and Disabled pass through unchanged. For
// distributions that bundle metrics-server (e.g. K3s), Default resolves
// to Enabled; otherwise it resolves to Disabled.
func (m *MetricsServer) EffectiveValue(distribution Distribution) MetricsServer {
	if *m != MetricsServerDefault && *m != "" {
		return *m
	}

	if distribution.ProvidesMetricsServerByDefault() {
		return MetricsServerEnabled
	}

	return MetricsServerDisabled
}
