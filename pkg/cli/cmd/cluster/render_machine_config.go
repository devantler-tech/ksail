package cluster

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	renderEndpointFlag = "endpoint"
	renderCertSANsFlag = "cert-sans"
	renderSecretsFlag  = "secrets"

	// renderControlPlaneFile and renderWorkerFile name the per-role outputs.
	renderControlPlaneFile = "controlplane.yaml"
	renderWorkerFile       = "worker.yaml"

	renderDirMode  = 0o750
	renderFileMode = 0o600
)

var (
	errRenderNotTalos      = errors.New("cluster render supports only the Talos distribution")
	errRenderNoTalosConfig = errors.New("talos machine configuration did not load")
)

const renderLongDesc = `Render the final Talos machine configuration offline.

Writes to <output-dir> the control-plane and worker machine configuration that cluster create
would apply to the first node of each role: Talos patches applied, KSail's
generated patches included, the installer image and post-installer
kernel-argument fold done, and the per-node hostname set. It uses KSail's own
pinned Talos machinery, so the output can be checked with
"talosctl validate --mode metal".

It starts no cluster, calls no cloud API and needs no credentials. The values
create learns from the cloud are flags instead: --endpoint (the first
control-plane address, or the floating IP), --cert-sans (the floating-IP SAN
set) and --secrets (a Talos secrets bundle; omitted, fresh PKI is generated as a
first create does). The Hetzner token in the floating-IP VIP block is always a
placeholder, so no credential is ever written to disk.

Supports the Talos distribution on the Hetzner provider.`

// renderFlags bundles the render command's own flags.
type renderFlags struct {
	endpoint string
	certSANs []string
	secrets  string
}

// NewRenderCmd creates the experimental cluster render command.
func NewRenderCmd() *cobra.Command {
	var flags renderFlags

	cmd := &cobra.Command{
		Use:   "render <output-dir>",
		Args:  cobra.ExactArgs(1),
		Short: "Render the final Talos machine configuration offline",
		Long:  renderLongDesc,
		Annotations: map[string]string{
			annotations.AnnotationDescription: "Render the Talos machine configuration " +
				"cluster create would apply, without creating a cluster",
		},
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		append(ksailconfigmanager.DefaultClusterFieldSelectors(),
			ksailconfigmanager.DefaultProviderFieldSelector(),
		),
	)

	// Render reads ksail.yaml as declared; the config-field flags stay hidden.
	cmd.Flags().VisitAll(func(f *pflag.Flag) { f.Hidden = true })

	cmd.Flags().StringVar(&flags.endpoint, renderEndpointFlag, "",
		"control-plane endpoint IP create would render (default: derived from the network CIDR)")
	cmd.Flags().StringSliceVar(&flags.certSANs, renderCertSANsFlag, nil,
		"certificate SANs create would render with the floating IP")
	cmd.Flags().StringVar(&flags.secrets, renderSecretsFlag, "",
		"Talos secrets bundle to render with (default: freshly generated PKI)")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runRender(cmd, cfgManager, args[0], flags)
	}

	return experimental.Guard(cmd)
}

func runRender(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	outputDir string,
	flags renderFlags,
) error {
	cluster, err := cfgManager.Load(configmanager.LoadOptions{Silent: true})
	if err != nil {
		return fmt.Errorf("failed to load cluster configuration: %w", err)
	}

	if cluster.Spec.Cluster.Distribution != v1alpha1.DistributionTalos {
		return fmt.Errorf("%w: got %q", errRenderNotTalos, cluster.Spec.Cluster.Distribution)
	}

	talosConfigs := cfgManager.DistributionConfig.Talos
	if talosConfigs == nil {
		return errRenderNoTalosConfig
	}

	opts := talosprovisioner.RenderOptions{
		EndpointIP: flags.endpoint,
		CertSANs:   flags.certSANs,
	}

	if flags.secrets != "" {
		opts.Secrets, err = loadRenderSecrets(flags.secrets)
		if err != nil {
			return err
		}
	}

	rendered, err := talosprovisioner.RenderMachineConfigs(
		talosConfigs, cluster.Spec.Cluster.Provider, cluster.Spec.Provider.Hetzner, opts,
	)
	if err != nil {
		return fmt.Errorf("failed to render Talos machine configuration: %w", err)
	}

	return writeRenderedConfigs(cmd, outputDir, rendered)
}

func loadRenderSecrets(path string) (*secrets.Bundle, error) {
	canonical, err := fsutil.EvalCanonicalPath(path)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve secrets bundle path: %w", err)
	}

	bundle, err := secrets.LoadBundle(canonical)
	if err != nil {
		return nil, fmt.Errorf("failed to load secrets bundle: %w", err)
	}

	return bundle, nil
}

func writeRenderedConfigs(
	cmd *cobra.Command,
	outputDir string,
	rendered *talosprovisioner.RenderedMachineConfigs,
) error {
	err := os.MkdirAll(outputDir, renderDirMode)
	if err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	dir, err := fsutil.EvalCanonicalPath(outputDir)
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	for _, file := range []struct {
		name, node string
		data       []byte
	}{
		{renderControlPlaneFile, rendered.ControlPlaneNode, rendered.ControlPlane},
		{renderWorkerFile, rendered.WorkerNode, rendered.Worker},
	} {
		path := filepath.Join(dir, file.name)

		err = os.WriteFile(path, file.data, renderFileMode)
		if err != nil {
			return fmt.Errorf("failed to write %s: %w", file.name, err)
		}

		notify.Infof(cmd.OutOrStdout(), "rendered %s (as node %s)", path, file.node)
	}

	return nil
}
