package reconcilediag

import (
	"context"
	"fmt"
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
	// Guard against unsupported engines before doing any filesystem or client
	// work — diagnostics are only meaningful for Flux and ArgoCD clusters.
	// This makes unsupported values a cheap no-op.
	if engine != v1alpha1.GitOpsEngineFlux && engine != v1alpha1.GitOpsEngineArgoCD {
		return
	}

	diagCtx, cancel := context.WithTimeout(ctx, diagnosticTimeout)
	defer cancel()

	dynClient, clientset, err := buildDiagClients(kubeconfigPath)
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
		// Guarded above; case required for exhaustive switch coverage.
	}

	if report != nil {
		report.Write(writer)
	}
}

// buildDiagClients creates the Kubernetes clients needed for diagnostics.
// Errors are returned to the caller, which treats them as best-effort skips.
func buildDiagClients(kubeconfigPath string) (dynamic.Interface, kubernetes.Interface, error) {
	canonPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	restCfg, err := k8sutil.BuildRESTConfig(canonPath, "")
	if err != nil {
		return nil, nil, fmt.Errorf("build REST config: %w", err)
	}

	dynClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create dynamic client: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("create clientset: %w", err)
	}

	return dynClient, clientset, nil
}
