package flux

import (
	"context"
	"time"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type kustomizationFlags struct {
	sourceKind      string
	sourceName      string
	path            string
	prune           bool
	targetNamespace string
	interval        time.Duration
	export          bool
	wait            bool
	dependsOn       []string
}

func (c *Client) newCreateKustomizationCmd() *cobra.Command {
	flags := &kustomizationFlags{
		interval: time.Minute,
		path:     "./",
	}

	cmd := &cobra.Command{
		Use:   "kustomization [name]",
		Short: "Create or update a Kustomization resource",
		Long:  "Create or update a Kustomization resource using Flux APIs",
		Example: `  # Create a Kustomization from a GitRepository source
  ksail workload create kustomization podinfo \
    --source=GitRepository/podinfo \
    --path="./kustomize" \
    --prune=true \
    --interval=5m

  # Create a Kustomization with a target namespace
  ksail workload create kustomization podinfo \
    --source=GitRepository/podinfo \
    --path="./kustomize" \
    --target-namespace=default \
    --prune=true`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, namespace := extractNameAndNamespace(cmd, args)

			return c.createKustomization(cmd.Context(), name, namespace, flags)
		},
	}

	cmd.Flags().
		StringVar(&flags.sourceKind, "source-kind", "GitRepository", "source kind (GitRepository, OCIRepository, Bucket)")
	cmd.Flags().
		StringVar(&flags.sourceName, "source", "", "source name in format 'Kind/name' or 'Kind/name.namespace'")
	cmd.Flags().
		StringVar(&flags.path, "path", "./", "path to the directory containing a kustomization.yaml file")
	cmd.Flags().BoolVar(&flags.prune, "prune", false, "enable garbage collection")
	cmd.Flags().BoolVar(&flags.wait, "wait", false, "enable health checking")
	cmd.Flags().
		StringVar(&flags.targetNamespace, "target-namespace", "", "overrides the namespace of all Kustomization objects")
	cmd.Flags().DurationVar(&flags.interval, "interval", time.Minute, "reconciliation interval")
	cmd.Flags().BoolVar(&flags.export, "export", false, "export in YAML format to stdout")
	cmd.Flags().
		StringSliceVar(&flags.dependsOn, "depends-on", nil, "Kustomization that must be ready before this one")

	_ = cmd.MarkFlagRequired("source")

	return cmd
}

func (c *Client) createKustomization(
	ctx context.Context,
	name, namespace string,
	flags *kustomizationFlags,
) error {
	sourceKind, sourceName, sourceNs := parseSourceRef(
		flags.sourceKind,
		flags.sourceName,
		namespace,
	)

	kustomization := &kustomizev1.Kustomization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: kustomizev1.KustomizationSpec{
			Interval: metav1.Duration{Duration: flags.interval},
			Path:     flags.path,
			Prune:    flags.prune,
			Wait:     flags.wait,
			SourceRef: kustomizev1.CrossNamespaceSourceReference{
				Kind:      sourceKind,
				Name:      sourceName,
				Namespace: sourceNs,
			},
		},
	}

	c.applyKustomizationOptions(kustomization, flags, namespace)

	// Export mode
	if flags.export {
		return c.exportResource(kustomization)
	}

	// Create or update the resource
	return c.upsertResource(ctx, kustomization, &kustomizev1.Kustomization{}, "Kustomization")
}

func (c *Client) applyKustomizationOptions(
	kustomization *kustomizev1.Kustomization,
	flags *kustomizationFlags,
	namespace string,
) {
	if flags.targetNamespace != "" {
		kustomization.Spec.TargetNamespace = flags.targetNamespace
	}

	if len(flags.dependsOn) > 0 {
		kustomization.Spec.DependsOn = parseDependencies(
			flags.dependsOn,
			namespace,
			func(depName, depNs string) kustomizev1.DependencyReference {
				return kustomizev1.DependencyReference{
					Name:      depName,
					Namespace: depNs,
				}
			},
		)
	}
}
