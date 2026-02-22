package registry

import (
	"context"
	"errors"
	"fmt"
	"io"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/docker/docker/client"
)

// Info describes a registry mirror that should be managed for a cluster.
type Info struct {
	Host     string
	Name     string
	Upstream string
	Port     int
	Volume   string
	Username string // Optional: username for registry authentication (supports ${ENV_VAR} placeholders)
	Password string
}

// Registry Lifecycle Management
// These functions handle the creation, setup, and cleanup of registry containers.

// PrepareRegistryManager builds a registry manager and extracts registry info via the provided extractor.
// The extractor receives the set of already used ports (if any) and should return the registries that need work.
// This function collects ports from ALL running containers (not just ksail-managed registries) to avoid
// port conflicts with registries created by other cluster distributions (e.g., K3d's native registries).
func PrepareRegistryManager(
	ctx context.Context,
	dockerClient client.APIClient,
	extractor func(baseUsedPorts map[int]struct{}) []Info,
) (Backend, []Info, error) {
	// Use the backend factory to create the registry manager
	backend, err := GetBackendFactory()(dockerClient)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create registry manager: %w", err)
	}

	registryInfos := extractor(nil)
	if len(registryInfos) == 0 {
		return nil, nil, nil
	}

	// Get used ports from ALL running containers to avoid conflicts with registries
	// created by other cluster distributions (e.g., K3d's native registries).
	usedPorts, err := CollectExistingRegistryPorts(ctx, backend)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get used host ports: %w", err)
	}

	if len(usedPorts) != 0 {
		registryInfos = extractor(usedPorts)
	}

	return backend, registryInfos, nil
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
		trimmed, ok := TrimNonEmpty(name)
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
		trimmed, ok := TrimNonEmpty(name)
		if !ok {
			continue
		}

		port, portErr := registryMgr.GetRegistryPort(ctx, trimmed)
		if portErr != nil {
			if errors.Is(portErr, dockerclient.ErrRegistryNotFound) ||
				errors.Is(portErr, dockerclient.ErrRegistryPortNotFound) {
				continue
			}

			return nil, fmt.Errorf(
				"failed to resolve port for registry %s: %w",
				trimmed,
				portErr,
			)
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
		Username:    reg.Username,
		Password:    reg.Password,
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
				Type: notify.ErrorType,
				Content: fmt.Sprintf(
					"failed to delete registry %s: %v",
					reg.Name,
					err,
				),
				Writer: writer,
			})
		}
	}
}
