package docker

import (
	"errors"
	"time"

	"github.com/docker/docker/client"
)

// Registry error definitions.
var (
	// ErrRegistryNotFound is returned when a registry container is not found.
	ErrRegistryNotFound = errors.New("registry not found")
	// ErrRegistryAlreadyExists is returned when trying to create a registry that already exists.
	ErrRegistryAlreadyExists = errors.New("registry already exists")
	// ErrRegistryPortNotFound is returned when the registry port cannot be determined.
	ErrRegistryPortNotFound = errors.New("registry port not found")
	// ErrRegistryNotReady is returned when a registry fails to become ready within the timeout.
	ErrRegistryNotReady = errors.New("registry not ready within timeout")
	// ErrRegistryUnexpectedStatus is returned when the registry returns an unexpected HTTP status.
	ErrRegistryUnexpectedStatus = errors.New("registry returned unexpected status")
	// ErrRegistryHealthCheckCancelled is returned when the health check is cancelled via context.
	ErrRegistryHealthCheckCancelled = errors.New("registry health check cancelled")
	// ErrRegistryPartialCredentials is returned when only username or password is provided.
	ErrRegistryPartialCredentials = errors.New(
		"registry proxy credentials incomplete: both username and password are required",
	)
)

const (
	// Registry image configuration.

	// RegistryImageName is the default registry image to use.
	RegistryImageName = "registry:3"

	// Registry labeling and identification.

	// RegistryLabelKey marks registry containers as managed by ksail.
	RegistryLabelKey = "io.ksail.registry"

	// Registry port configuration.

	// DefaultRegistryPort is the default port for registry containers.
	DefaultRegistryPort = 5000
	// RegistryPortBase is the base port number for calculating registry ports.
	RegistryPortBase = 5000
	// HostPortParts is the expected number of parts in a host:port string.
	HostPortParts = 2
	// RegistryContainerPort is the internal port exposed by the registry container.
	RegistryContainerPort = "5000/tcp"
	// RegistryHostIP is the host IP address to bind registry ports to.
	RegistryHostIP = "127.0.0.1"

	// Registry container configuration.

	// RegistryDataPath is the path inside the container where registry data is stored.
	RegistryDataPath = "/var/lib/registry"
	// RegistryRestartPolicy defines the container restart policy.
	RegistryRestartPolicy = "unless-stopped"

	// Registry health check configuration.

	// RegistryReadyTimeout is the maximum time to wait for a registry to become ready.
	RegistryReadyTimeout = 30 * time.Second
	// RegistryReadyPollInterval is the interval between registry health checks.
	RegistryReadyPollInterval = 500 * time.Millisecond
	// RegistryHTTPTimeout is the timeout for individual HTTP health check requests.
	RegistryHTTPTimeout = 2 * time.Second
	// ConnectionRefusedCheckThreshold is the number of consecutive connection refused errors
	// before checking if the container has crashed.
	ConnectionRefusedCheckThreshold = 5
)

// RegistryManager manages Docker registry containers for mirror/pull-through caching.
type RegistryManager struct {
	client client.APIClient
}

// NewRegistryManager creates a new RegistryManager.
func NewRegistryManager(apiClient client.APIClient) (*RegistryManager, error) {
	if apiClient == nil {
		return nil, ErrAPIClientNil
	}

	return &RegistryManager{
		client: apiClient,
	}, nil
}

// RegistryConfig holds configuration for creating a registry.
type RegistryConfig struct {
	Name        string
	Port        int
	UpstreamURL string
	ClusterName string
	NetworkName string
	VolumeName  string
	Username    string // Optional: username for upstream registry authentication (supports ${ENV_VAR} placeholders)
	Password    string // Optional: password for upstream registry authentication (supports ${ENV_VAR} placeholders)
}
