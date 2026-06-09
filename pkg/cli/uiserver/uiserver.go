// Package uiserver builds and binds the local KSail web UI server shared by the `ksail ui` command
// and the KSail desktop app. It serves the embedded SPA together with a REST API backed by the local
// cluster lifecycle, bound to loopback only.
package uiserver

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/devantler-tech/ksail/v7/pkg/cli/clusterapi"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/webui"
)

// Host is the loopback address the UI binds to. Binding only to localhost is the security boundary:
// the local API is unauthenticated and must not be reachable from the network.
const Host = "127.0.0.1"

// NewServer returns the API server that serves the web UI plus a REST API backed by the local
// cluster lifecycle (Docker-based and, where credentials are present, cloud providers).
func NewServer() *api.Server {
	service := clusterapi.NewService()

	server := &api.Server{
		Service:       service,
		ReadOnly:      false,
		Mode:          api.ModeLocal,
		Distributions: clusterapi.CreatableDistributions(),
		StaticFS:      webui.Assets(),
		// Gate the create form on which providers this machine can actually reach (Docker running,
		// HCLOUD_TOKEN set, eksctl installed, …). The operator leaves this nil and offers all providers.
		ProviderStatus: func(ctx context.Context) []api.ProviderInfo {
			return toProviderInfos(service.Availability(ctx))
		},
	}

	// Resolve credentials through the OS secure store + Settings overrides (falling back to plain
	// environment resolution when the store or settings file is unavailable), and expose the Settings
	// page so credentials can be configured without shell env — important for a Dock/Finder-launched
	// desktop app, which does not inherit the shell environment.
	manager, secureStorageAvailable := newCredentialManager()
	if manager != nil {
		service.UseCredentials(manager)
		server.Settings = settingsService{
			manager:                manager,
			secureStorageAvailable: secureStorageAvailable,
		}
	}

	return server
}

// toProviderInfos maps the discovery availability report onto the API's wire type.
func toProviderInfos(availabilities []clusterdiscovery.Availability) []api.ProviderInfo {
	infos := make([]api.ProviderInfo, len(availabilities))
	for index, availability := range availabilities {
		infos[index] = api.ProviderInfo{
			Name:      string(availability.Provider),
			Available: availability.Available,
			Reason:    availability.Reason,
		}
	}

	return infos
}

// Listen binds a loopback listener on the given port (0 picks a free port) and returns the listener
// together with the URL it serves. Callers bind first so they can learn the chosen port before
// serving and opening a browser or window.
func Listen(ctx context.Context, port int) (net.Listener, string, error) {
	bindAddr := net.JoinHostPort(Host, strconv.Itoa(port))

	var listenConfig net.ListenConfig

	listener, err := listenConfig.Listen(ctx, "tcp", bindAddr)
	if err != nil {
		return nil, "", fmt.Errorf("listen on %s: %w", bindAddr, err)
	}

	_, boundPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		_ = listener.Close()

		return nil, "", fmt.Errorf(
			"determine bound port from %q: %w",
			listener.Addr().String(),
			err,
		)
	}

	url := fmt.Sprintf("http://%s/", net.JoinHostPort(Host, boundPort))

	return listener, url, nil
}
