package workload

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/client/helm"
	configmanagerinterface "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/spf13/cobra"
)

// ErrUnknownOutputFormat is returned when an unrecognized output format is specified.
var ErrUnknownOutputFormat = errors.New("unknown output format")

const imagesCmdLong = `List container images required by the configured cluster components.

The image list is derived from the ksail.yaml configuration and includes
images for all enabled components (GitOps engine, CNI, policy engine, etc.).

This command is useful for:
  - Pre-pulling images before cluster creation
  - Creating offline image archives
  - Understanding infrastructure image requirements
  - CI/CD caching strategies

Output formats:
  - plain: One image per line (default, suitable for scripting)
  - json: JSON array of image strings

Examples:
  # List all images for current ksail.yaml config
  ksail workload images

  # List images as JSON array
  ksail workload images --output=json

  # Pipe to docker pull
  ksail workload images | xargs -n1 docker pull

  # Save to file for CI caching
  ksail workload images > required-images.txt`

// NewImagesCmd creates the command to list required container images.
func NewImagesCmd() *cobra.Command {
	var outputFormat string

	cmd := &cobra.Command{
		Use:          "images",
		Short:        "List container images required by cluster components",
		Long:         imagesCmdLong,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
	}

	// Create config manager with full field selectors to detect all components
	cfgManager := createImagesConfigManager(cmd)

	cmd.Flags().StringVarP(&outputFormat, "output", "o", "plain",
		"Output format: plain, json")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runImagesCommand(cmd, cfgManager, outputFormat)
	}

	return cmd
}

// createImagesConfigManager creates a config manager for the images command.
// It needs to load the full cluster config to understand which components are enabled.
func createImagesConfigManager(cmd *cobra.Command) *configmanager.ConfigManager {
	fieldSelectors := []configmanager.FieldSelector[v1alpha1.Cluster]{
		configmanager.DefaultDistributionFieldSelector(),
		configmanager.DefaultProviderFieldSelector(),
		configmanager.DefaultCNIFieldSelector(),
		configmanager.DefaultCSIFieldSelector(),
		configmanager.DefaultMetricsServerFieldSelector(),
		configmanager.DefaultLoadBalancerFieldSelector(),
		configmanager.DefaultCertManagerFieldSelector(),
		configmanager.DefaultPolicyEngineFieldSelector(),
		configmanager.DefaultGitOpsEngineFieldSelector(),
	}

	return configmanager.NewCommandConfigManager(cmd, fieldSelectors)
}

func runImagesCommand(
	cmd *cobra.Command,
	cfgManager *configmanager.ConfigManager,
	outputFormat string,
) error {
	tmr := timer.New()
	tmr.Start()

	outputTimer := helpers.MaybeTimer(cmd, tmr)

	clusterCfg, err := cfgManager.Load(configmanagerinterface.LoadOptions{
		Silent:         true,
		SkipValidation: true,
	})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Create Helm client for dynamic image extraction from Helm charts
	// Uses template-only client that doesn't require kubeconfig
	helmClient, err := helm.NewTemplateOnlyClient()
	if err != nil {
		return fmt.Errorf("create helm client: %w", err)
	}

	// Use Factory to get images dynamically from Helm charts
	factory := installer.NewFactory(
		helmClient,
		nil, // dockerClient not needed for image extraction
		"",  // kubeconfig not needed for image extraction
		"",  // kubecontext not needed for image extraction
		0,
		clusterCfg.Spec.Cluster.Distribution,
	)

	images, err := factory.GetImagesForCluster(cmd.Context(), clusterCfg)
	if err != nil {
		return fmt.Errorf("extract images from installers: %w", err)
	}

	// Sort and deduplicate for consistent, stable output
	slices.Sort(images)
	images = slices.Compact(images)

	// Output based on format
	switch strings.ToLower(outputFormat) {
	case "json":
		return outputJSON(cmd, images, outputTimer)
	case "plain", "":
		return outputPlain(cmd, images, outputTimer)
	default:
		return fmt.Errorf("%w: %s (valid: plain, json)", ErrUnknownOutputFormat, outputFormat)
	}
}

func outputPlain(cmd *cobra.Command, images []string, tmr timer.Timer) error {
	if len(images) == 0 {
		// Write warning to stderr to keep stdout clean for scripting (e.g., xargs docker pull)
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "no images required for current configuration",
			Timer:   tmr,
			Writer:  cmd.ErrOrStderr(),
		})

		return nil
	}

	for _, img := range images {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), img)
		if err != nil {
			return fmt.Errorf("write image to stdout: %w", err)
		}
	}

	return nil
}

func outputJSON(cmd *cobra.Command, images []string, _ timer.Timer) error {
	data, err := json.Marshal(images)
	if err != nil {
		return fmt.Errorf("marshal images to JSON: %w", err)
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
	if err != nil {
		return fmt.Errorf("write JSON to stdout: %w", err)
	}

	return nil
}
