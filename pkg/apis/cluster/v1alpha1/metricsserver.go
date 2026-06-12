package v1alpha1

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

// ValidMetricsServers returns supported metrics server values.
func ValidMetricsServers() []MetricsServer {
	return []MetricsServer{
		MetricsServerDefault,
		MetricsServerEnabled,
		MetricsServerDisabled,
	}
}

// Set for MetricsServer (pflag.Value interface).
func (m *MetricsServer) Set(value string) error {
	return setEnum(m, value, ValidMetricsServers(), ErrInvalidMetricsServer)
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
	return validValueStrings(ValidMetricsServers())
}
