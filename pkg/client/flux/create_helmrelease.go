package flux

import (
	"context"
	"strings"
	"time"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type helmReleaseFlags struct {
	sourceKind      string
	sourceName      string
	chart           string
	chartVersion    string
	targetNamespace string
	createNamespace bool
	interval        time.Duration
	export          bool
	dependsOn       []string
}

func (c *Client) newCreateHelmReleaseCmd() *cobra.Command {
	flags := &helmReleaseFlags{
		interval: time.Minute,
	}

	cmd := &cobra.Command{
		Use:     "helmrelease [name]",
		Aliases: []string{"hr"},
		Short:   "Create or update a HelmRelease resource",
		Long:    "Create or update a HelmRelease resource using Flux APIs",
		Example: `  # Create a HelmRelease with a chart from a HelmRepository source
  ksail workload create helmrelease podinfo \
    --source=HelmRepository/podinfo \
    --chart=podinfo \
    --chart-version=6.6.2 \
    --namespace=flux-system

  # Create a HelmRelease targeting a different namespace
  ksail workload create helmrelease podinfo \
    --source=HelmRepository/podinfo \
    --chart=podinfo \
    --target-namespace=production \
    --create-target-namespace=true`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, namespace := extractNameAndNamespace(cmd, args)

			return c.createHelmRelease(cmd.Context(), name, namespace, flags)
		},
	}

	cmd.Flags().
		StringVar(&flags.sourceKind, "source-kind", "HelmRepository", "source kind (HelmRepository, GitRepository, Bucket)")
	cmd.Flags().
		StringVar(&flags.sourceName, "source", "", "source name in format 'Kind/name' or 'Kind/name.namespace'")
	cmd.Flags().StringVar(&flags.chart, "chart", "", "Helm chart name or path")
	cmd.Flags().StringVar(&flags.chartVersion, "chart-version", "", "Helm chart version")
	cmd.Flags().
		StringVar(&flags.targetNamespace, "target-namespace", "", "namespace to install the Helm release")
	cmd.Flags().
		BoolVar(&flags.createNamespace, "create-target-namespace", false, "create the target namespace if it doesn't exist")
	cmd.Flags().DurationVar(&flags.interval, "interval", time.Minute, "reconciliation interval")
	cmd.Flags().BoolVar(&flags.export, "export", false, "export in YAML format to stdout")
	cmd.Flags().
		StringSliceVar(&flags.dependsOn, "depends-on", nil, "HelmRelease that must be ready before this one")

	_ = cmd.MarkFlagRequired("source")
	_ = cmd.MarkFlagRequired("chart")

	return cmd
}

func (c *Client) createHelmRelease(
	ctx context.Context,
	name, namespace string,
	flags *helmReleaseFlags,
) error {
	sourceKind, sourceName, sourceNs := parseSourceRef(
		flags.sourceKind,
		flags.sourceName,
		namespace,
	)

	helmRelease := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			Interval: metav1.Duration{Duration: flags.interval},
			Chart: &helmv2.HelmChartTemplate{
				Spec: helmv2.HelmChartTemplateSpec{
					Chart: flags.chart,
					SourceRef: helmv2.CrossNamespaceObjectReference{
						Kind:      sourceKind,
						Name:      sourceName,
						Namespace: sourceNs,
					},
				},
			},
		},
	}

	c.applyHelmReleaseOptions(helmRelease, flags, namespace)

	// Export mode
	if flags.export {
		return c.exportResource(helmRelease)
	}

	// Create or update the resource
	return c.upsertResource(ctx, helmRelease, &helmv2.HelmRelease{}, "HelmRelease")
}

func (c *Client) applyHelmReleaseOptions(
	helmRelease *helmv2.HelmRelease,
	flags *helmReleaseFlags,
	namespace string,
) {
	if flags.chartVersion != "" {
		helmRelease.Spec.Chart.Spec.Version = flags.chartVersion
	}

	if flags.targetNamespace != "" {
		helmRelease.Spec.TargetNamespace = flags.targetNamespace
	}

	if flags.createNamespace {
		helmRelease.Spec.Install = &helmv2.Install{
			CreateNamespace: true,
		}
	}

	if len(flags.dependsOn) > 0 {
		helmRelease.Spec.DependsOn = parseDependencies(
			flags.dependsOn,
			namespace,
			func(depName, depNs string) helmv2.DependencyReference {
				return helmv2.DependencyReference{
					Name:      depName,
					Namespace: depNs,
				}
			},
		)
	}
}

func parseSourceRef(sourceKind, sourceName, defaultNamespace string) (string, string, string) {
	kind := sourceKind
	name := sourceName
	namespace := defaultNamespace

	if strings.Contains(sourceName, "/") {
		parts := strings.SplitN(sourceName, "/", SplitParts)
		if len(parts) == SplitParts {
			kind = parts[0]
			name = parts[1]
		}
	}

	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", SplitParts)
		if len(parts) == SplitParts {
			name = parts[0]
			namespace = parts[1]
		}
	}

	return kind, name, namespace
}

func parseDependencies[T any](
	dependsOn []string,
	defaultNamespace string,
	factory func(name, namespace string) T,
) []T {
	deps := make([]T, 0, len(dependsOn))

	for _, dep := range dependsOn {
		depName := dep
		depNs := defaultNamespace

		if strings.Contains(dep, "/") {
			parts := strings.SplitN(dep, "/", SplitParts)
			if len(parts) == SplitParts {
				depNs = parts[0]
				depName = parts[1]
			}
		}

		deps = append(deps, factory(depName, depNs))
	}

	return deps
}
