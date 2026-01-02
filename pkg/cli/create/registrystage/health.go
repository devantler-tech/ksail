package registrystage

import (
	"context"
	"fmt"
	"io"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/docker/docker/client"
)

// WaitForRegistriesReady waits for mirror registries to become ready.
// This is a shared helper used by Kind, K3d, and Talos registry stages.
func WaitForRegistriesReady(
	ctx context.Context,
	dockerAPIClient client.APIClient,
	registryInfos []registry.Info,
	writer io.Writer,
) error {
	if len(registryInfos) == 0 {
		return nil
	}

	registryMgr, err := dockerclient.NewRegistryManager(dockerAPIClient)
	if err != nil {
		return fmt.Errorf("failed to create registry manager: %w", err)
	}

	// Build registry name map for health check
	registryIPs := make(map[string]string, len(registryInfos))
	for _, info := range registryInfos {
		registryIPs[info.Name] = ""
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "waiting for mirror registries to become ready",
		Writer:  writer,
	})

	err = registryMgr.WaitForRegistriesReady(ctx, registryIPs)
	if err != nil {
		return fmt.Errorf("failed waiting for registries to become ready: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "all mirror registries are ready",
		Writer:  writer,
	})

	return nil
}
