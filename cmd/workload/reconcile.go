package workload

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/pkg/client/argocd"
	cmdhelpers "github.com/devantler-tech/ksail/pkg/cmd"
	runtime "github.com/devantler-tech/ksail/pkg/di"
	iopath "github.com/devantler-tech/ksail/pkg/io"
	ksailconfigmanager "github.com/devantler-tech/ksail/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/pkg/k8s"
	"github.com/devantler-tech/ksail/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail/pkg/ui/notify"
	"github.com/devantler-tech/ksail/pkg/ui/timer"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var errLocalRegistryRequired = errors.New(
	"local registry and a gitops engine must be enabled to reconcile workloads; " +
		"enable it with '--local-registry Enabled' and '--gitops-engine Flux|ArgoCD' " +
		"during cluster init or set 'spec.localRegistry: Enabled' and " +
		"'spec.gitOpsEngine: Flux' in ksail.yaml",
)

var errGitOpsEngineRequired = errors.New(
	"a gitops engine must be enabled to reconcile workloads; " +
		"enable it with '--gitops-engine Flux|ArgoCD' during cluster init or " +
		"set 'spec.gitOpsEngine: Flux|ArgoCD' in ksail.yaml",
)

const (
	fluxNamespace             = "flux-system"
	fluxRootKustomizationName = "flux-system"
	argoCDNamespace           = "argocd"
	argoCDRootApplicationName = "ksail"
	defaultReconcileTimeout   = 5 * time.Minute
	reconcilePollInterval     = 2 * time.Second
)

// reconcileFluxKustomization triggers and waits for Flux kustomization reconciliation.
func reconcileFluxKustomization(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	timeout time.Duration,
	outputTimer timer.Timer,
	writer io.Writer,
) error {
	kubeconfigPath := strings.TrimSpace(clusterCfg.Spec.Connection.Kubeconfig)
	if kubeconfigPath == "" {
		kubeconfigPath = v1alpha1.DefaultKubeconfigPath
	}

	kubeconfigPath, err := iopath.ExpandHomePath(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("expand kubeconfig path: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "triggering flux kustomization reconciliation",
		Timer:   outputTimer,
		Writer:  writer,
	})

	// Create dynamic client
	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, "")
	if err != nil {
		return fmt.Errorf("build rest config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create dynamic client: %w", err)
	}

	// Define the Kustomization GVR
	kustomizationGVR := schema.GroupVersionResource{
		Group:    "kustomize.toolkit.fluxcd.io",
		Version:  "v1",
		Resource: "kustomizations",
	}

	// Get the kustomization
	kustomizationClient := dynamicClient.Resource(kustomizationGVR).Namespace(fluxNamespace)
	kustomization, err := kustomizationClient.Get(ctx, fluxRootKustomizationName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get flux kustomization: %w", err)
	}

	// Annotate to trigger reconciliation
	annotations := kustomization.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
	}
	annotations["reconcile.fluxcd.io/requestedAt"] = time.Now().Format(time.RFC3339Nano)
	kustomization.SetAnnotations(annotations)

	// Update the kustomization
	_, err = kustomizationClient.Update(ctx, kustomization, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("trigger flux reconciliation: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "waiting for flux kustomization to reconcile",
		Timer:   outputTimer,
		Writer:  writer,
	})

	// Wait for reconciliation to complete
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(reconcilePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for flux kustomization reconciliation")
		case <-ticker.C:
			kustomization, err = kustomizationClient.Get(ctx, fluxRootKustomizationName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get flux kustomization status: %w", err)
			}

			// Check if ready
			conditions, found, err := unstructured.NestedSlice(kustomization.Object, "status", "conditions")
			if err != nil || !found {
				continue
			}

			for _, condition := range conditions {
				condMap, ok := condition.(map[string]interface{})
				if !ok {
					continue
				}

				condType, _, _ := unstructured.NestedString(condMap, "type")
				condStatus, _, _ := unstructured.NestedString(condMap, "status")

				if condType == "Ready" && condStatus == "True" {
					return nil
				}
			}
		}
	}
}

// reconcileArgoCDApplication refreshes and waits for the ArgoCD application to sync.
func reconcileArgoCDApplication(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	artifactVersion string,
	timeout time.Duration,
	outputTimer timer.Timer,
	writer io.Writer,
) error {
	kubeconfigPath := strings.TrimSpace(clusterCfg.Spec.Connection.Kubeconfig)
	if kubeconfigPath == "" {
		kubeconfigPath = v1alpha1.DefaultKubeconfigPath
	}

	kubeconfigPath, err := iopath.ExpandHomePath(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("expand kubeconfig path: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "triggering argocd application refresh",
		Timer:   outputTimer,
		Writer:  writer,
	})

	argocdMgr, err := argocd.NewManagerFromKubeconfig(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("create argocd manager: %w", err)
	}

	err = argocdMgr.UpdateTargetRevision(ctx, argocd.UpdateTargetRevisionOptions{
		TargetRevision: artifactVersion,
		HardRefresh:    true,
	})
	if err != nil {
		return fmt.Errorf("refresh argocd application: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: "waiting for argocd application to sync",
		Timer:   outputTimer,
		Writer:  writer,
	})

	// Create dynamic client to watch application status
	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, "")
	if err != nil {
		return fmt.Errorf("build rest config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create dynamic client: %w", err)
	}

	// Define the Application GVR
	applicationGVR := schema.GroupVersionResource{
		Group:    "argoproj.io",
		Version:  "v1alpha1",
		Resource: "applications",
	}

	// Wait for application to sync and become healthy
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(reconcilePollInterval)
	defer ticker.Stop()

	applicationClient := dynamicClient.Resource(applicationGVR).Namespace(argoCDNamespace)

	for {
		select {
		case <-timeoutCtx.Done():
			return fmt.Errorf("timeout waiting for argocd application sync")
		case <-ticker.C:
			app, err := applicationClient.Get(ctx, argoCDRootApplicationName, metav1.GetOptions{})
			if err != nil {
				return fmt.Errorf("get argocd application status: %w", err)
			}

			// Check sync status
			syncStatus, found, err := unstructured.NestedString(app.Object, "status", "sync", "status")
			if err != nil || !found {
				continue
			}

			// Check health status
			healthStatus, found, err := unstructured.NestedString(app.Object, "status", "health", "status")
			if err != nil || !found {
				continue
			}

			if syncStatus == "Synced" && healthStatus == "Healthy" {
				return nil
			}
		}
	}
}

// NewReconcileCmd creates the workload reconcile command.
func NewReconcileCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "reconcile",
		Short:        "Trigger reconciliation for GitOps workloads",
		Long:         "Trigger reconciliation/sync for the root Flux kustomization or root ArgoCD application.",
		SilenceUsage: true,
	}

	cmd.Flags().Duration("timeout", 0, "timeout for waiting for reconciliation to complete (overrides config timeout)")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		tmr := timer.New()
		tmr.Start()

		fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
		cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, fieldSelectors)

		outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

		clusterCfg, err := cfgManager.LoadConfig(outputTimer)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		gitOpsEngineConfigured := clusterCfg.Spec.GitOpsEngine != v1alpha1.GitOpsEngineNone

		if !gitOpsEngineConfigured {
			return errGitOpsEngineRequired
		}

		// Determine timeout: flag > config > default
		timeout, err := cmd.Flags().GetDuration("timeout")
		if err != nil {
			return fmt.Errorf("get timeout flag: %w", err)
		}

		if timeout == 0 {
			// Check config timeout
			if clusterCfg.Spec.Connection.Timeout.Duration > 0 {
				timeout = clusterCfg.Spec.Connection.Timeout.Duration
			} else {
				timeout = defaultReconcileTimeout
			}
		}

		artifactVersion := registry.DefaultLocalArtifactTag

		cmd.Println()
		notify.WriteMessage(notify.Message{
			Type:    notify.TitleType,
			Emoji:   "🔄",
			Content: "Trigger Reconciliation...",
			Writer:  cmd.OutOrStdout(),
		})

		tmr.NewStage()

		if clusterCfg.Spec.GitOpsEngine == v1alpha1.GitOpsEngineArgoCD {
			err = reconcileArgoCDApplication(
				cmd.Context(),
				clusterCfg,
				artifactVersion,
				timeout,
				outputTimer,
				cmd.OutOrStdout(),
			)
			if err != nil {
				return err
			}
		} else if clusterCfg.Spec.GitOpsEngine == v1alpha1.GitOpsEngineFlux {
			err = reconcileFluxKustomization(
				cmd.Context(),
				clusterCfg,
				timeout,
				outputTimer,
				cmd.OutOrStdout(),
			)
			if err != nil {
				return err
			}
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: "reconciliation completed",
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		return nil
	}

	return cmd
}
