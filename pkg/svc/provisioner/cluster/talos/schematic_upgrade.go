package talosprovisioner

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/state"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosimages "github.com/siderolabs/talos/pkg/images"
	"github.com/siderolabs/talos/pkg/machinery/constants"
	"github.com/siderolabs/talos/pkg/machinery/resources/runtime"
)

var _ clusterupdate.DistributionImagePlanner = (*Provisioner)(nil)

// DistributionImageChanged checks every managed machine's booted schematic, including
// when none is configured: clearing the schematic selects the default installer image,
// so a node still booted from a custom one must roll too. Docker cannot install
// factory images in place and Omni owns its own upgrades.
func (p *Provisioner) DistributionImageChanged(
	ctx context.Context,
	clusterName string,
) (bool, error) {
	desired := p.resolveSchematicID()
	if p.hetznerOpts == nil || p.omniOpts != nil {
		return false, nil
	}

	nodes, err := p.getNodesByRole(ctx, p.resolveClusterName(clusterName))
	if err != nil {
		return false, fmt.Errorf("listing nodes for schematic check: %w", err)
	}

	return schematicsChanged(ctx, nodes, desired, p.getRunningSchematic)
}

// Read all identities even after finding drift: an unreadable remaining node
// must fail the preflight before the first machine is drained or rebooted.
func schematicsChanged(
	ctx context.Context, nodes []nodeWithRole, desired string,
	read func(context.Context, string) (string, error),
) (bool, error) {
	if len(nodes) == 0 {
		return false, clustererr.ErrNoNodesFound
	}

	changed := false

	for _, node := range nodes {
		running, err := read(ctx, node.IP)
		if err != nil {
			return false, fmt.Errorf("reading schematic on %s: %w", node.IP, err)
		}

		changed = changed || !schematicMatches(running, desired)
	}

	return changed, nil
}

func (p *Provisioner) getRunningSchematic(ctx context.Context, nodeIP string) (string, error) {
	var schematic string

	err := p.retryTransientTalosAPICall(
		ctx,
		nodeIP,
		"schematic check",
		func(ctx context.Context) error {
			client, err := p.createTalosClient(ctx, nodeIP)
			if err != nil {
				return err
			}
			defer client.Close() //nolint:errcheck

			schematic, err = schematicFromState(ctx, client.COSI)

			return err
		},
	)
	if err != nil {
		return "", fmt.Errorf("schematic check for node %s: %w", nodeIP, err)
	}

	return schematic, nil
}

// ExtensionStatus describes the running image, unlike machine.install.image,
// which only describes a future install. It is available before Talos 1.14's
// ImageFactorySchematic resource as well. A node booted from a non-factory image
// carries no schematic extension and reads as "", while a malformed or duplicated
// identity is undetermined.
func schematicFromState(ctx context.Context, resourceState state.State) (string, error) {
	items, err := resourceState.List(ctx, resource.NewMetadata(
		runtime.NamespaceName, runtime.ExtensionStatusType, "", resource.VersionUndefined,
	))
	if err != nil {
		return "", fmt.Errorf("listing running extensions: %w", err)
	}

	schematic := ""

	for _, item := range items.Items {
		extension, ok := item.(*runtime.ExtensionStatus)
		if !ok {
			return "", ErrSchematicUndetermined
		}

		metadata := extension.TypedSpec().Metadata
		if metadata.Name != constants.ImageFactorySchematicExtensionName {
			continue
		}

		decoded, decodeErr := hex.DecodeString(metadata.Version)
		if decodeErr != nil || len(decoded) != 32 || schematic != "" {
			return "", ErrSchematicUndetermined
		}

		schematic = metadata.Version
	}

	return schematic, nil
}

// schematicMatches reports whether a node booted the desired schematic. With none
// configured KSail installs the default image, which boots either with no factory
// identity (the legacy ghcr.io installer) or with the factory's empty schematic.
func schematicMatches(running, desired string) bool {
	if desired == "" {
		return running == "" || running == talosimages.DefaultInstallerImageSchematic
	}

	return running == desired
}

func runningImageMatchesTarget(
	ctx context.Context,
	resourceState state.State,
	runningVersion, desiredVersion, desiredSchematic string,
) (bool, error) {
	if !runningVersionMatchesTarget(runningVersion, desiredVersion) {
		return false, nil
	}

	running, err := schematicFromState(ctx, resourceState)
	if err != nil {
		return false, err
	}

	return schematicMatches(running, desiredSchematic), nil
}

func (p *Provisioner) nodeImageMatchesTarget(
	ctx context.Context, nodeIP, desiredVersion string,
) (bool, error) {
	var matches bool

	err := p.retryTransientTalosAPICall(
		ctx,
		nodeIP,
		"boot image check",
		func(ctx context.Context) error {
			client, err := p.createTalosClient(ctx, nodeIP)
			if err != nil {
				return err
			}
			defer client.Close() //nolint:errcheck

			version, err := versionTagFromClient(ctx, client)
			if err != nil {
				return err
			}

			matches, err = runningImageMatchesTarget(
				ctx,
				client.COSI,
				version,
				desiredVersion,
				p.resolveSchematicID(),
			)

			return err
		},
	)

	return matches, err
}
