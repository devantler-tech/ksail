package talosprovisioner

import (
	"context"
	"errors"
	"fmt"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	factoryclient "github.com/siderolabs/image-factory/pkg/client"
)

// imageFactoryURL is the Image Factory that serves KSail's installer and snapshot images.
const imageFactoryURL = "https://factory.talos.dev"

// ErrSchematicIDMismatch reports that Image Factory stored KSail's schematic under an ID other
// than the one KSail computed, so images addressed by the computed ID would not be served.
var ErrSchematicIDMismatch = errors.New("image factory stored the schematic under a different ID")

// registerWithImageFactory sends a schematic to Image Factory and returns its stored ID.
func registerWithImageFactory(
	ctx context.Context,
	computed talosconfigmanager.Schematic,
) (string, error) {
	client, err := factoryclient.New(imageFactoryURL)
	if err != nil {
		return "", fmt.Errorf("create image factory client: %w", err)
	}

	schematicID, _, err := client.SchematicCreate(ctx, computed)
	if err != nil {
		return "", fmt.Errorf("register schematic with image factory: %w", err)
	}

	return schematicID, nil
}

// ensureSchematicRegistered registers KSail's computed schematic with Image Factory before
// schematicID is used for an image, because the factory serves images only for schematics it
// has been sent (#7132). Registration is idempotent, and the factory must store it under the
// computed ID. An explicitly configured schematicId that KSail did not compute is left as it is.
func (p *Provisioner) ensureSchematicRegistered(ctx context.Context, schematicID string) error {
	if p.talosConfigs == nil || p.talosConfigs.Schematic() == nil ||
		schematicID != p.talosConfigs.SchematicID() {
		return nil
	}

	registeredID, err := p.schematicRegistrar(ctx, *p.talosConfigs.Schematic())
	if err != nil {
		return fmt.Errorf("registering Talos schematic %s: %w", schematicID, err)
	}

	if registeredID != schematicID {
		return fmt.Errorf(
			"%w: computed %s, image factory returned %q",
			ErrSchematicIDMismatch, schematicID, registeredID,
		)
	}

	return nil
}
