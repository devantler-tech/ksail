package nested

import (
	"context"
	"fmt"
	"os"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"k8s.io/client-go/kubernetes"
)

// DumpFailureDiagnostics is an opt-in (KSAIL_NESTED_DEBUG) diagnostic that dumps
// the given namespaces' pod states, events, and logs to stdout when a nested
// cluster fails to come up. It reveals why the nested control-plane pod(s) fail
// to become Ready on a given host (image pull, scheduling, or crash). It is a
// no-op unless KSAIL_NESTED_DEBUG is set.
//
// Pass the cluster namespace plus any operator/system namespaces relevant to the
// distribution (e.g. K3d also passes the k3k-system namespace; VCluster passes
// only the cluster namespace).
func DumpFailureDiagnostics(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespaces ...string,
) {
	if !DebugEnabled() {
		return
	}

	_, _ = fmt.Fprint(os.Stdout, k8s.DumpNamespaceDiagnostics(ctx, clientset, namespaces...))
}
