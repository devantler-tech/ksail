package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail-go/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

var (
	// ErrNameRequired indicates that an operation was attempted without providing a registry name.
	ErrNameRequired = errors.New("registry name is required")
	// ErrHostRequired indicates that no host or bind address was provided for the registry endpoint.
	ErrHostRequired = errors.New("registry host is required")
	// ErrInvalidPort indicates that a provided registry port is outside the valid TCP port range.
	ErrInvalidPort = errors.New("registry port must be between 1 and 65535")
)

// Service models the lifecycle management interface for localhost-scoped OCI registry.
type Service interface {
	// Create provisions (or updates) an OCI registry container definition using the supplied options.
	Create(ctx context.Context, opts CreateOptions) (v1alpha1.OCIRegistry, error)
	// Start ensures the registry container is running and optionally attached to the target network.
	Start(ctx context.Context, opts StartOptions) (v1alpha1.OCIRegistry, error)
	// Stop halts the running registry container and optionally removes persistent storage resources.
	Stop(ctx context.Context, opts StopOptions) error
	// Status inspects the registry container and returns its current lifecycle state and metadata.
	Status(ctx context.Context, opts StatusOptions) (v1alpha1.OCIRegistry, error)
}

const (
	// DefaultEndpointHost exposes the registry on the developer workstation only.
	DefaultEndpointHost = "localhost"
	minRegistryPort     = 1
	maxRegistryPort     = 65535
)

// CreateOptions capture the desired shape of a managed OCI registry container.
type CreateOptions struct {
	Name        string
	Host        string
	Port        int
	VolumeName  string
	ClusterName string
}

// WithDefaults applies standard defaults for host bindings and storage metadata.
func (o CreateOptions) WithDefaults() CreateOptions {
	trimmed := o
	trimmed.Name = strings.TrimSpace(trimmed.Name)
	trimmed.Host = strings.TrimSpace(trimmed.Host)
	trimmed.VolumeName = strings.TrimSpace(trimmed.VolumeName)

	if trimmed.Host == "" {
		trimmed.Host = DefaultEndpointHost
	}

	if trimmed.VolumeName == "" {
		trimmed.VolumeName = trimmed.Name
	}

	return trimmed
}

// Validate asserts that the option set is well formed before interacting with the container engine.
func (o CreateOptions) Validate() error {
	opts := o.WithDefaults()

	if opts.Name == "" {
		return ErrNameRequired
	}

	if opts.Port < minRegistryPort || opts.Port > maxRegistryPort {
		return fmt.Errorf("%w: %d", ErrInvalidPort, opts.Port)
	}

	if opts.Host == "" {
		return ErrHostRequired
	}

	return nil
}

// Endpoint returns the host-facing endpoint string (host:port) for the registry.
func (o CreateOptions) Endpoint() string {
	opts := o.WithDefaults()
	if opts.Port <= 0 {
		return opts.Host
	}

	return net.JoinHostPort(opts.Host, strconv.Itoa(opts.Port))
}

// StartOptions define how a registry instance should be started or connected.
type StartOptions struct {
	Name        string
	NetworkName string
}

// Validate ensures the start options reference a registry container.
func (o StartOptions) Validate() error {
	if strings.TrimSpace(o.Name) == "" {
		return ErrNameRequired
	}

	return nil
}

// StopOptions describe how a registry instance should be stopped and optionally cleaned up.
type StopOptions struct {
	Name         string
	ClusterName  string
	NetworkName  string
	DeleteVolume bool
	VolumeName   string
}

// Validate ensures the stop options reference a registry container.
func (o StopOptions) Validate() error {
	if strings.TrimSpace(o.Name) == "" {
		return ErrNameRequired
	}

	return nil
}

// StatusOptions identify the registry instance whose status should be inspected.
type StatusOptions struct {
	Name string
}

// Validate ensures the status query references a registry container.
func (o StatusOptions) Validate() error {
	if strings.TrimSpace(o.Name) == "" {
		return ErrNameRequired
	}

	return nil
}

// Config controls how the registry service interacts with the container engine.
type Config struct {
	DockerClient    client.APIClient
	RegistryManager Backend
}

type service struct {
	docker   client.APIClient
	registry Backend
	manager  *Manager
}

// errDockerClientRequired ensures service construction validates inputs.
var errDockerClientRequired = errors.New("docker client is required")

// NewService constructs a registry lifecycle manager backed by the Docker API.
func NewService(cfg Config) (Service, error) {
	if cfg.DockerClient == nil {
		return nil, errDockerClientRequired
	}

	backend := cfg.RegistryManager
	if backend == nil {
		manager, err := dockerclient.NewRegistryManager(cfg.DockerClient)
		if err != nil {
			return nil, fmt.Errorf("create registry manager: %w", err)
		}

		backend = manager
	}

	controller, err := NewManager(backend)
	if err != nil {
		return nil, fmt.Errorf("create registry controller: %w", err)
	}

	return &service{
		docker:   cfg.DockerClient,
		registry: backend,
		manager:  controller,
	}, nil
}

func (s *service) Create(ctx context.Context, opts CreateOptions) (v1alpha1.OCIRegistry, error) {
	err := opts.Validate()
	if err != nil {
		return v1alpha1.NewOCIRegistry(), err
	}

	resolved := opts.WithDefaults()

	_, ensureErr := s.manager.EnsureOne(
		ctx,
		resolved.toRegistryInfo(),
		resolved.ClusterName,
		io.Discard,
	)
	if ensureErr != nil {
		model := buildRegistryModel(
			resolved.Name,
			resolved.Endpoint(),
			int32(resolved.Port), //nolint:gosec // port validated to <= 65535
			resolved.VolumeName,
		)
		model.Status = v1alpha1.OCIRegistryStatusError
		model.LastError = ensureErr.Error()

		return model, fmt.Errorf("create registry: %w", ensureErr)
	}

	return s.Status(ctx, StatusOptions{Name: resolved.Name})
}

func (s *service) Start(ctx context.Context, opts StartOptions) (v1alpha1.OCIRegistry, error) {
	validateErr := opts.Validate()
	if validateErr != nil {
		return v1alpha1.NewOCIRegistry(), validateErr
	}

	summary, err := s.findRegistryContainer(ctx, opts.Name)
	if err != nil {
		return v1alpha1.NewOCIRegistry(), err
	}

	if !strings.EqualFold(summary.State, "running") {
		err := s.docker.ContainerStart(ctx, summary.ID, container.StartOptions{})
		if err != nil {
			return v1alpha1.NewOCIRegistry(), fmt.Errorf("start registry container: %w", err)
		}
	}

	if networkName := strings.TrimSpace(opts.NetworkName); networkName != "" {
		networkErr := s.ensureNetworkAttachment(ctx, summary.ID, networkName)
		if networkErr != nil {
			return v1alpha1.NewOCIRegistry(), networkErr
		}
	}

	return s.Status(ctx, StatusOptions{Name: opts.Name})
}

func (s *service) Stop(ctx context.Context, opts StopOptions) error {
	validateErr := opts.Validate()
	if validateErr != nil {
		return validateErr
	}

	cleanupErr := s.manager.CleanupOne(
		ctx,
		Info{Name: opts.Name, Volume: strings.TrimSpace(opts.VolumeName)},
		opts.ClusterName,
		opts.DeleteVolume,
		opts.NetworkName,
	)
	if cleanupErr != nil {
		if errors.Is(cleanupErr, dockerclient.ErrRegistryNotFound) {
			return nil
		}

		return fmt.Errorf("cleanup registry %s: %w", opts.Name, cleanupErr)
	}

	return nil
}

func (o CreateOptions) toRegistryInfo() Info {
	opts := o.WithDefaults()

	return Info{
		Host:   opts.Host,
		Name:   opts.Name,
		Port:   opts.Port,
		Volume: opts.VolumeName,
	}
}

func (s *service) Status(ctx context.Context, opts StatusOptions) (v1alpha1.OCIRegistry, error) {
	validateErr := opts.Validate()
	if validateErr != nil {
		return v1alpha1.NewOCIRegistry(), validateErr
	}

	model := v1alpha1.NewOCIRegistry()
	model.Name = opts.Name
	model.DataPath = dockerclient.RegistryDataPath

	containers, err := s.listRegistryContainers(ctx, opts.Name)
	if err != nil {
		return model, err
	}

	if len(containers) == 0 {
		return model, nil
	}

	summary := containers[0]
	model.VolumeName = extractVolumeName(summary, opts.Name)

	hostPort := resolveHostPort(summary)
	if hostPort == 0 {
		port, portErr := s.registry.GetRegistryPort(ctx, opts.Name)
		if portErr != nil {
			switch {
			case errors.Is(portErr, dockerclient.ErrRegistryNotFound),
				errors.Is(portErr, dockerclient.ErrRegistryPortNotFound):
				// No additional metadata available.
			default:
				return model, fmt.Errorf("get registry port: %w", portErr)
			}
		} else {
			hostPort = port
		}
	}

	if hostPort > 0 {
		model.Port = int32(hostPort) //nolint:gosec // port originates from Docker metadata
		host := resolveEndpointHost(summary)
		model.Endpoint = net.JoinHostPort(host, strconv.Itoa(hostPort))
	}

	model.Status, model.LastError = mapContainerState(summary.State, summary.Status)

	return model, nil
}

func (s *service) listRegistryContainers(
	ctx context.Context,
	name string,
) ([]container.Summary, error) {
	filtersArgs := filters.NewArgs()
	filtersArgs.Add("name", name)
	filtersArgs.Add("ancestor", dockerclient.RegistryImageName)
	filtersArgs.Add("label", fmt.Sprintf("%s=%s", dockerclient.RegistryLabelKey, name))

	containers, err := s.docker.ContainerList(
		ctx,
		container.ListOptions{All: true, Filters: filtersArgs},
	)
	if err != nil {
		return nil, fmt.Errorf("list registry containers: %w", err)
	}

	return containers, nil
}

func (s *service) findRegistryContainer(
	ctx context.Context,
	name string,
) (container.Summary, error) {
	containers, err := s.listRegistryContainers(ctx, name)
	if err != nil {
		return container.Summary{}, err
	}

	if len(containers) == 0 {
		return container.Summary{}, dockerclient.ErrRegistryNotFound
	}

	return containers[0], nil
}

func (s *service) ensureNetworkAttachment(
	ctx context.Context,
	containerID, networkName string,
) error {
	inspect, err := s.docker.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("inspect registry container: %w", err)
	}

	if inspect.NetworkSettings != nil {
		if _, exists := inspect.NetworkSettings.Networks[networkName]; exists {
			return nil
		}
	}

	connectErr := s.docker.NetworkConnect(
		ctx,
		networkName,
		containerID,
		&network.EndpointSettings{},
	)
	if connectErr != nil {
		return fmt.Errorf("connect registry to network %s: %w", networkName, connectErr)
	}

	return nil
}

func buildRegistryModel(name, endpoint string, port int32, volumeName string) v1alpha1.OCIRegistry {
	model := v1alpha1.NewOCIRegistry()
	model.Name = name
	model.Endpoint = endpoint
	model.Port = port
	model.VolumeName = volumeName
	model.DataPath = dockerclient.RegistryDataPath
	model.Status = v1alpha1.OCIRegistryStatusProvisioning

	return model
}

func extractVolumeName(summary container.Summary, fallback string) string {
	for _, mountPoint := range summary.Mounts {
		if mountPoint.Type == mount.TypeVolume && strings.TrimSpace(mountPoint.Name) != "" {
			return mountPoint.Name
		}
	}

	return fallback
}

func resolveHostPort(summary container.Summary) int {
	for _, port := range summary.Ports {
		if port.PrivatePort == dockerclient.DefaultRegistryPort && port.PublicPort > 0 {
			return int(port.PublicPort)
		}
	}

	return 0
}

func resolveEndpointHost(summary container.Summary) string {
	if len(summary.Ports) == 0 {
		return dockerclient.RegistryHostIP
	}

	host := strings.TrimSpace(summary.Ports[0].IP)
	if host == "" {
		return dockerclient.RegistryHostIP
	}

	return host
}

func mapContainerState(state, status string) (v1alpha1.OCIRegistryStatus, string) {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "created", "restarting", "running":
		return v1alpha1.OCIRegistryStatusRunning, ""
	case "removing", "exited", "dead":
		if trimmed := strings.TrimSpace(status); trimmed != "" {
			return v1alpha1.OCIRegistryStatusError, trimmed
		}

		return v1alpha1.OCIRegistryStatusError, "registry container not running"
	default:
		return v1alpha1.OCIRegistryStatusError, strings.TrimSpace(status)
	}
}
