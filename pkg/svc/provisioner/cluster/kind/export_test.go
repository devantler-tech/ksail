package kindprovisioner

import (
	"context"

	"sigs.k8s.io/kind/pkg/log"
)

// KubeConfigForTest returns the kubeConfig field for testing purposes.
func (k *Provisioner) KubeConfigForTest() string {
	return k.kubeConfig
}

// WithWaitForReadyForTest injects a stub readiness waiter so Start can be
// exercised without a live cluster.
func (k *Provisioner) WithWaitForReadyForTest(
	f func(ctx context.Context, kubeconfigPath, contextName string) error,
) *Provisioner {
	k.waitForReady = f

	return k
}

// NewStreamLoggerForTest creates a streamLogger for testing.
func NewStreamLoggerForTest(w interface {
	Write(p []byte) (n int, err error)
},
) log.Logger {
	return &streamLogger{writer: w}
}

// SetNameForTest exposes setName for unit testing.
func SetNameForTest(name, kindConfigName string) string {
	return setName(name, kindConfigName)
}
