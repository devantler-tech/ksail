package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	"github.com/siderolabs/talos/pkg/cluster"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/client-go/rest"
)

const (
	kubernetesUpgradeReadAttempts = 6
	kubernetesUpgradeReadWait     = time.Second
	kubernetesUpgradeReadMaxWait  = 5 * time.Second
)

var errKubernetesAPIUnavailable = errors.New(
	"the Kubernetes API server stopped serving requests (it restarts during upgrades) and did not recover",
)

type upgradeKubernetesProvider struct {
	cluster.K8sProvider

	wait, maxWait time.Duration
}

func kubernetesUpgradeProvider(provider cluster.K8sProvider) cluster.K8sProvider {
	return upgradeKubernetesProvider{
		K8sProvider: provider,
		wait:        kubernetesUpgradeReadWait,
		maxWait:     kubernetesUpgradeReadMaxWait,
	}
}

// K8sRestConfig covers the SSA inventory read and manifest dry runs, which run
// while the API server may still be restarting from the preceding upgrade step.
// Copy the config and compose its transport wrapper so the original provider
// and its authentication remain intact.
func (provider upgradeKubernetesProvider) K8sRestConfig(ctx context.Context) (*rest.Config, error) {
	config, err := provider.K8sProvider.K8sRestConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("getting Kubernetes upgrade REST config: %w", err)
	}

	config = rest.CopyConfig(config)
	config.Wrap(func(transport http.RoundTripper) http.RoundTripper {
		return upgradeReadTransport{
			transport: transport,
			wait:      provider.wait,
			maxWait:   provider.maxWait,
		}
	})

	return config, nil
}

type upgradeReadTransport struct {
	transport     http.RoundTripper
	wait, maxWait time.Duration
}

//nolint:wrapcheck // A transport decorator preserves the underlying request errors.
func (transport upgradeReadTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	retryable := upgradeReplayPolicy(request)
	if retryable == nil {
		return transport.transport.RoundTrip(request)
	}

	var response *http.Response

	attempt := 0

	err := netretry.Do(request.Context(), kubernetesUpgradeReadAttempts,
		transport.wait, transport.maxWait, func() error {
			err := request.Context().Err()
			if err != nil {
				return err
			}

			current, err := replayUpgradeRequest(request, attempt)
			if err != nil {
				return err
			}

			attempt++

			//nolint:bodyclose // Successful response is returned to the caller.
			response, err = transport.transport.RoundTrip(current)
			if err != nil && response != nil {
				if response.Body != nil {
					_ = response.Body.Close()
				}

				response = nil
			}

			return err
		}, netretry.WithRetryable(retryable))
	if err != nil && !netretry.IsCancelled(err) && retryable(err) {
		return nil, apiServerUnavailableError{cause: err}
	}

	return response, err
}

// apiServerUnavailableError reports an exhausted replay in plain terms, while
// errors.Is still reaches both the sentinel and the transport failure.
type apiServerUnavailableError struct {
	cause error
}

func (err apiServerUnavailableError) Error() string {
	return fmt.Sprintf("%v within %d attempts",
		errKubernetesAPIUnavailable, kubernetesUpgradeReadAttempts)
}

func (err apiServerUnavailableError) Unwrap() []error {
	return []error{errKubernetesAPIUnavailable, err.cause}
}

// upgradeReplayPolicy returns which errors may replay a request, or nil when
// it must never be replayed. Only requests the API server never persists
// qualify: bodyless reads, and dry runs whose body can be rewound.
func upgradeReplayPolicy(request *http.Request) func(error) bool {
	hasBody := request.Body != nil && request.Body != http.NoBody

	switch {
	case slices.Contains(request.URL.Query()["dryRun"], metav1.DryRunAll) &&
		(!hasBody || request.GetBody != nil):
		// client-go replays neither a GOAWAY nor a reset for a write, dry run or not.
		return isAPIServerRestartError
	case request.Method == http.MethodGet && !hasBody:
		// client-go already handles EOF and connection resets for reads.
		return isConnectionRefused
	default:
		return nil
	}
}

func isConnectionRefused(err error) bool {
	return errors.Is(err, errKubernetesUpgradeConnectionRefused)
}

func isAPIServerRestartError(err error) bool {
	return isConnectionRefused(err) || utilnet.IsConnectionReset(err) || utilnet.IsProbableEOF(err)
}

func replayUpgradeRequest(request *http.Request, attempt int) (*http.Request, error) {
	if attempt == 0 || request.GetBody == nil {
		return request, nil
	}

	body, err := request.GetBody()
	if err != nil {
		return nil, fmt.Errorf("rewinding Kubernetes upgrade request body: %w", err)
	}

	replay := request.Clone(request.Context())
	replay.Body = body

	return replay, nil
}
