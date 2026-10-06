package vclusterprovisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

const vclusterAPIRequestTimeout = 15 * time.Second

// errUnexpectedReadyzResponse reports a successful readiness request whose body was not "ok".
var errUnexpectedReadyzResponse = errors.New("unexpected /readyz response")

// mergeReadyVClusterKubeconfig verifies the rewritten endpoint using the generated
// credentials before publishing it. A populated Secret alone does not establish
// that the NodePort or Gateway is ready to serve the next command.
func mergeReadyVClusterKubeconfig(
	ctx context.Context,
	path string,
	kubeconfigData []byte,
	interval, timeout time.Duration,
) error {
	config, err := clientcmd.RESTConfigFromKubeConfig(kubeconfigData)
	if err != nil {
		return fmt.Errorf("build vCluster REST config: %w", err)
	}

	config.Timeout = vclusterAPIRequestTimeout

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("create vCluster API client: %w", err)
	}

	// The final probe is usually cut short by the deadline, so the last answer the
	// endpoint actually gave is kept apart from the last transport error.
	var lastProbeErr, lastResponseErr error

	err = wait.PollUntilContextTimeout(
		ctx,
		interval,
		timeout,
		true,
		func(ctx context.Context) (bool, error) {
			body, probeErr := client.Discovery().RESTClient().Get().AbsPath("/readyz").DoRaw(ctx)
			if probeErr != nil {
				lastProbeErr = probeErr

				return false, nil
			}

			got := strings.TrimSpace(string(body))
			if got != "ok" {
				lastProbeErr = nil
				lastResponseErr = fmt.Errorf("%w: %q", errUnexpectedReadyzResponse, got)

				return false, nil
			}

			return true, nil
		},
	)
	if err != nil {
		return fmt.Errorf(
			"wait for vCluster API server: %w",
			errors.Join(err, lastProbeErr, lastResponseErr),
		)
	}

	if path == "" {
		return nil
	}

	err = k8s.MergeKubeconfig(path, kubeconfigData)
	if err != nil {
		return fmt.Errorf("merge vCluster kubeconfig: %w", err)
	}

	return nil
}
