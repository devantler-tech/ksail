package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	"github.com/siderolabs/talos/pkg/cluster"
	"k8s.io/client-go/rest"
)

const (
	kubernetesUpgradeReadAttempts = 6
	kubernetesUpgradeReadWait     = time.Second
	kubernetesUpgradeReadMaxWait  = 5 * time.Second
)

type upgradeKubernetesProvider struct {
	cluster.K8sProvider
}

func kubernetesUpgradeProvider(provider cluster.K8sProvider) cluster.K8sProvider {
	return upgradeKubernetesProvider{K8sProvider: provider}
}

// K8sRestConfig covers the SSA inventory read, which precedes the SDK's
// reconciliation retry loop. Copy the config and compose its transport wrapper
// so the original provider and its authentication remain intact.
func (provider upgradeKubernetesProvider) K8sRestConfig(ctx context.Context) (*rest.Config, error) {
	config, err := provider.K8sProvider.K8sRestConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Kubernetes upgrade REST config: %w", err)
	}

	config = rest.CopyConfig(config)
	config.Wrap(func(transport http.RoundTripper) http.RoundTripper {
		return upgradeReadTransport{transport: transport}
	})

	return config, nil
}

type upgradeReadTransport struct {
	transport http.RoundTripper
}

//nolint:wrapcheck // A transport decorator preserves the underlying request errors.
func (transport upgradeReadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// Never replay a mutation or a request body. client-go already handles EOF
	// and connection resets for reads; add only the observed API restart gap.
	if request.Method != http.MethodGet || (request.Body != nil && request.Body != http.NoBody) {
		return transport.transport.RoundTrip(request)
	}

	var response *http.Response

	err := netretry.Do(request.Context(), kubernetesUpgradeReadAttempts,
		kubernetesUpgradeReadWait, kubernetesUpgradeReadMaxWait, func() error {
			err := request.Context().Err()
			if err != nil {
				return err
			}

			//nolint:bodyclose // Successful response is returned to the caller.
			response, err = transport.transport.RoundTrip(request)
			if err != nil && response != nil {
				if response.Body != nil {
					_ = response.Body.Close()
				}

				response = nil
			}

			return err
		}, netretry.WithRetryable(func(err error) bool {
			return errors.Is(err, errKubernetesUpgradeConnectionRefused)
		}))

	return response, err
}
