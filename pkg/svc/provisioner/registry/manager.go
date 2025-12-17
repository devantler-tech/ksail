package registry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	dockerclient "github.com/devantler-tech/ksail-go/pkg/client/docker"
	ksailio "github.com/devantler-tech/ksail-go/pkg/io"
	"github.com/devantler-tech/ksail-go/pkg/ui/notify"
	"github.com/docker/docker/client"
)

// Info describes a registry mirror that should be managed for a cluster.
type Info struct {
	Host     string
	Name     string
	Upstream string
	Port     int
	Volume   string
}

// DefaultRegistryPort defines the default container registry port inside the container.
const DefaultRegistryPort = 5000

const expectedEndpointParts = 2

// Registry Lifecycle Management
// These functions handle the creation, setup, and cleanup of registry containers.

// PrepareRegistryManager builds a registry manager and extracts registry info via the provided extractor.
// The extractor receives the set of already used ports (if any) and should return the registries that need work.
func PrepareRegistryManager(
	ctx context.Context,
	dockerClient client.APIClient,
	extractor func(baseUsedPorts map[int]struct{}) []Info,
) (*dockerclient.RegistryManager, []Info, error) {
	regManager, err := dockerclient.NewRegistryManager(dockerClient)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create registry manager: %w", err)
	}

	registryInfos := extractor(nil)
	if len(registryInfos) == 0 {
		return nil, nil, nil
	}

	existingPorts, err := CollectExistingRegistryPorts(ctx, regManager)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to collect existing registry ports: %w", err)
	}

	if len(existingPorts) != 0 {
		registryInfos = extractor(existingPorts)
	}

	return regManager, registryInfos, nil
}

// SetupRegistries ensures that the provided registries exist. Any newly created
// registries are cleaned up if a later creation fails.
func SetupRegistries(
	ctx context.Context,
	registryMgr Backend,
	registries []Info,
	clusterName string,
	networkName string,
	writer io.Writer,
) error {
	if registryMgr == nil || len(registries) == 0 {
		return nil
	}

	batch, err := newMirrorBatch(
		ctx,
		registryMgr,
		clusterName,
		networkName,
		writer,
		len(registries),
	)
	if err != nil {
		return fmt.Errorf("create registry batch: %w", err)
	}

	for _, reg := range registries {
		_, ensureErr := batch.ensure(ctx, reg)
		if ensureErr != nil {
			batch.rollback(ctx)

			return fmt.Errorf("ensure registry %s: %w", reg.Name, ensureErr)
		}
	}

	return nil
}

type mirrorBatch struct {
	manager     Backend
	existing    map[string]struct{}
	created     []Info
	clusterName string
	networkName string
	writer      io.Writer
}

func newMirrorBatch(
	ctx context.Context,
	mgr Backend,
	clusterName string,
	networkName string,
	writer io.Writer,
	estimatedRegistries int,
) (*mirrorBatch, error) {
	existingRegistries, err := collectExistingRegistryNames(ctx, mgr)
	if err != nil {
		return nil, err
	}

	if estimatedRegistries < 0 {
		estimatedRegistries = 0
	}

	return &mirrorBatch{
		manager:     mgr,
		existing:    existingRegistries,
		created:     make([]Info, 0, estimatedRegistries),
		clusterName: clusterName,
		networkName: networkName,
		writer:      writer,
	}, nil
}

func (b *mirrorBatch) ensure(ctx context.Context, reg Info) (bool, error) {
	created, err := ensureRegistry(
		ctx,
		b.manager,
		b.clusterName,
		reg,
		b.writer,
		b.existing,
	)
	if err != nil {
		return false, err
	}

	if created {
		b.created = append(b.created, reg)
	}

	return created, nil
}

func (b *mirrorBatch) rollback(ctx context.Context) {
	if len(b.created) == 0 {
		return
	}

	cleanupCreatedRegistries(
		ctx,
		b.manager,
		b.created,
		b.clusterName,
		b.networkName,
		b.writer,
	)
}

func collectExistingRegistryNames(
	ctx context.Context,
	registryMgr Backend,
) (map[string]struct{}, error) {
	existingRegistries := make(map[string]struct{})

	current, listErr := registryMgr.ListRegistries(ctx)
	if listErr != nil {
		return nil, fmt.Errorf("failed to list existing registries: %w", listErr)
	}

	for _, name := range current {
		trimmed, ok := ksailio.TrimNonEmpty(name)
		if !ok {
			continue
		}

		existingRegistries[trimmed] = struct{}{}
	}

	return existingRegistries, nil
}

// CollectExistingRegistryPorts returns a set of host ports that are already bound by
// existing registry containers. This allows callers to avoid host port collisions
// when provisioning new registry mirrors for additional clusters.
func CollectExistingRegistryPorts(
	ctx context.Context,
	registryMgr Backend,
) (map[int]struct{}, error) {
	ports := make(map[int]struct{})

	if registryMgr == nil {
		return ports, nil
	}

	names, err := registryMgr.ListRegistries(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list existing registries: %w", err)
	}

	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			continue
		}

		port, portErr := registryMgr.GetRegistryPort(ctx, trimmed)
		if portErr != nil {
			switch {
			case errors.Is(portErr, dockerclient.ErrRegistryNotFound),
				errors.Is(portErr, dockerclient.ErrRegistryPortNotFound):
				continue
			default:
				return nil, fmt.Errorf(
					"failed to resolve port for registry %s: %w",
					trimmed,
					portErr,
				)
			}
		}

		if port > 0 {
			ports[port] = struct{}{}
		}
	}

	return ports, nil
}

func ensureRegistry(
	ctx context.Context,
	registryMgr Backend,
	clusterName string,
	reg Info,
	writer io.Writer,
	existing map[string]struct{},
) (bool, error) {
	_, alreadyExists := existing[reg.Name]

	message := notify.Message{
		Type:   notify.ActivityType,
		Writer: writer,
	}

	if alreadyExists {
		message.Content = "skipping '%s' as it already exists"
		message.Args = []any{reg.Name}
	} else {
		message.Content = "creating '%s' for '%s'"
		message.Args = []any{reg.Name, reg.Upstream}
	}

	notify.WriteMessage(message)

	config := dockerclient.RegistryConfig{
		Name:        reg.Name,
		Port:        reg.Port,
		UpstreamURL: reg.Upstream,
		ClusterName: clusterName,
		NetworkName: "",
		VolumeName:  reg.Volume,
	}

	err := registryMgr.CreateRegistry(ctx, config)
	if err != nil {
		return false, fmt.Errorf("failed to create registry %s: %w", reg.Name, err)
	}

	if alreadyExists {
		return false, nil
	}

	existing[reg.Name] = struct{}{}

	return true, nil
}

func cleanupCreatedRegistries(
	ctx context.Context,
	registryMgr Backend,
	created []Info,
	clusterName string,
	networkName string,
	writer io.Writer,
) {
	for i := len(created) - 1; i >= 0; i-- {
		reg := created[i]

		err := registryMgr.DeleteRegistry(
			ctx,
			reg.Name,
			clusterName,
			false,
			networkName,
			reg.Volume,
		)
		if err != nil {
			notify.WriteMessage(notify.Message{
				Type: notify.WarningType,
				Content: fmt.Sprintf(
					"cleanup warning: failed to delete registry %s: %v",
					reg.Name,
					err,
				),
				Writer: writer,
			})
		}
	}
}

// Network Operations
// These functions manage network connections between registry containers and clusters.

// ConnectRegistriesToNetwork attaches each registry container to the provided network.
// Any connection failures are logged as warnings but do not abort the operation.
func ConnectRegistriesToNetwork(
	ctx context.Context,
	dockerClient client.APIClient,
	registries []Info,
	networkName string,
	writer io.Writer,
) error {
	networkName, networkOK := ksailio.TrimNonEmpty(networkName)
	if dockerClient == nil || len(registries) == 0 || !networkOK {
		return nil
	}

	for _, reg := range registries {
		containerName, nameOK := ksailio.TrimNonEmpty(reg.Name)
		if !nameOK {
			continue
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "connecting '%s' to '%s'",
			Writer:  writer,
			Args: []any{
				containerName,
				networkName,
			},
		})

		err := dockerClient.NetworkConnect(ctx, networkName, containerName, nil)
		if err != nil {
			notify.WriteMessage(notify.Message{
				Type: notify.WarningType,
				Content: fmt.Sprintf(
					"failed to connect registry %s to %s network: %v",
					containerName,
					networkName,
					err,
				),
				Writer: writer,
			})
		}
	}

	return nil
}

// CleanupRegistries removes the provided registry. Errors are logged as warnings.
func CleanupRegistries(
	ctx context.Context,
	registryMgr Backend,
	registries []Info,
	clusterName string,
	deleteVolumes bool,
	networkName string,
	warningWriter io.Writer,
) error {
	if registryMgr == nil || len(registries) == 0 {
		return nil
	}

	writer := warningWriter
	if writer == nil {
		writer = os.Stderr
	}

	for _, reg := range registries {
		err := registryMgr.DeleteRegistry(
			ctx,
			reg.Name,
			clusterName,
			deleteVolumes,
			networkName,
			reg.Volume,
		)
		if err != nil {
			_, _ = fmt.Fprintf(
				writer,
				"Warning: failed to cleanup registry %s: %v\n",
				reg.Name,
				err,
			)
		}
	}

	return nil
}

// Naming and Identification Utilities
// These functions handle registry naming conventions and identifier generation.

// SanitizeHostIdentifier converts a registry host string into a Docker-safe identifier while keeping dots intact
// so hosts such as docker.io remain reachable via container name resolution.
func SanitizeHostIdentifier(host string) string {
	sanitized := strings.ReplaceAll(host, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, ":", "-")

	return sanitized
}

// GenerateVolumeName returns a deterministic Docker volume name for the registry.
func GenerateVolumeName(host string) string {
	return SanitizeHostIdentifier(host)
}

// GenerateUpstreamURL attempts to derive the upstream registry URL from the host name.
func GenerateUpstreamURL(host string) string {
	if host == "docker.io" {
		return "https://registry-1.docker.io"
	}

	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return host
	}

	return "https://" + host
}

// ExtractRegistryPort determines a unique host port to expose for the given endpoints.
func ExtractRegistryPort(endpoints []string, usedPorts map[int]struct{}, nextPort *int) int {
	if nextPort == nil {
		defaultPort := DefaultRegistryPort
		nextPort = &defaultPort
	}

	if candidate := firstAvailableEndpointPort(endpoints, usedPorts, nextPort); candidate > 0 {
		return candidate
	}

	port := *nextPort
	for {
		if port <= 0 {
			port = DefaultRegistryPort
		}

		if _, exists := usedPorts[port]; !exists {
			break
		}

		port++
	}

	usedPorts[port] = struct{}{}
	*nextPort = port + 1

	return port
}

func firstAvailableEndpointPort(
	endpoints []string,
	usedPorts map[int]struct{},
	nextPort *int,
) int {
	if len(endpoints) == 0 {
		return 0
	}

	extracted := ExtractPortFromEndpoint(endpoints[0])
	if extracted <= 0 {
		return 0
	}

	if _, exists := usedPorts[extracted]; exists {
		return 0
	}

	usedPorts[extracted] = struct{}{}
	if extracted >= *nextPort {
		*nextPort = extracted + 1
	}

	return extracted
}

// ExtractPortFromEndpoint extracts the port from an endpoint URL. Returns 0 if not found.
func ExtractPortFromEndpoint(endpoint string) int {
	lastColon := strings.LastIndex(endpoint, ":")
	if lastColon < 0 {
		return 0
	}

	portStr := endpoint[lastColon+1:]
	if slashIdx := strings.Index(portStr, "/"); slashIdx >= 0 {
		portStr = portStr[:slashIdx]
	}

	var port int

	_, err := fmt.Sscanf(portStr, "%d", &port)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}

	return port
}

// ResolveRegistryName determines the registry container name from endpoints or falls back to prefix + host.
func ResolveRegistryName(host string, endpoints []string, prefix string) string {
	expected := SanitizeHostIdentifier(host)

	for _, endpoint := range endpoints {
		name := ExtractNameFromEndpoint(endpoint)
		if name == "" {
			continue
		}

		if expected != "" && strings.EqualFold(name, expected) {
			return name
		}
	}

	return BuildRegistryName(prefix, host)
}

// ExtractNameFromEndpoint extracts the hostname portion from an endpoint URL.
func ExtractNameFromEndpoint(endpoint string) string {
	parts := strings.Split(endpoint, "//")
	if len(parts) != expectedEndpointParts {
		return ""
	}

	hostPort := strings.Split(parts[1], ":")
	if len(hostPort) == 0 {
		return ""
	}

	return hostPort[0]
}

// BuildRegistryName constructs a registry container name from prefix and host.
func BuildRegistryName(prefix, host string) string {
	sanitized := SanitizeHostIdentifier(host)

	return prefix + sanitized
}

// Registry Configuration Builders
// These functions construct Info structures from various inputs.

// BuildRegistryInfo creates an Info populated with derived fields using the supplied prefix for container names.
func BuildRegistryInfo(
	host string,
	endpoints []string,
	port int,
	prefix string,
	upstreamOverride string,
) Info {
	name := ResolveRegistryName(host, endpoints, prefix)

	upstream := strings.TrimSpace(upstreamOverride)
	if upstream == "" {
		upstream = GenerateUpstreamURL(host)
	}

	volume := GenerateVolumeName(host)

	return Info{
		Host:     host,
		Name:     name,
		Upstream: upstream,
		Port:     port,
		Volume:   volume,
	}
}

// SortHosts deterministically sorts registry hostnames.
func SortHosts(hosts []string) {
	sort.Strings(hosts)
}

// CollectRegistryNames extracts registry names from a slice of registry Info structs.
// Empty names are filtered out. This is useful for collecting names before cleanup operations.
func CollectRegistryNames(infos []Info) []string {
	names := make([]string, 0, len(infos))

	for _, reg := range infos {
		name := strings.TrimSpace(reg.Name)
		if name == "" {
			continue
		}

		names = append(names, name)
	}

	return names
}
