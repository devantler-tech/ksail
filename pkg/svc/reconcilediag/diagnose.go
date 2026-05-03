package reconcilediag

import (
	"context"
	"io"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	k8sutil "github.com/devantler-tech/ksail/v7/pkg/k8s"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
)

// diagnosticTimeout limits how long we spend collecting diagnostics so a
// broken cluster doesn't hang the CLI indefinitely.
const diagnosticTimeout = 15 * time.Second

// Diagnose collects and writes a targeted diagnostic report for a failed
// GitOps reconciliation. It is best-effort: if client creation or collection
// fails, the error is silently swallowed to avoid masking the original
// reconciliation error.
func Diagnose(
	ctx context.Context,
	writer io.Writer,
	kubeconfigPath string,
	engine v1alpha1.GitOpsEngine,
) {
	diagCtx, cancel := context.WithTimeout(ctx, diagnosticTimeout)
	defer cancel()

	canonPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return
	}

	restCfg, err := k8sutil.BuildRESTConfig(canonPath, "")
	if err != nil {
		return
	}

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return
	}

	var report *Report

	switch engine {
	case v1alpha1.GitOpsEngineFlux:
		collector := &FluxCollector{Dynamic: dynClient, Clientset: clientset}
		report = collector.Collect(diagCtx)
	case v1alpha1.GitOpsEngineArgoCD:
		collector := &ArgoCDCollector{Dynamic: dynClient, Clientset: clientset}
		report = collector.Collect(diagCtx)
	case v1alpha1.GitOpsEngineNone:
		return
	default:
		return
	}

	if report == nil || report.IsEmpty() {
		return
	}

	report.Write(writer)
}
