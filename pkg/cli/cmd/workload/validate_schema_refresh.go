package workload

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubeconform"
	"github.com/spf13/cobra"
)

// runValidateCmd gives an explicit refresh one private cache for the complete
// validation operation. Persistent entries change only when every stage succeeds.
func runValidateCmd(
	ctx context.Context,
	cmd *cobra.Command,
	args []string,
	flags validateFlags,
) error {
	if !flags.refreshSchemas || flags.schemaRefresh != nil {
		return runValidateCmdWithSchemaCache(ctx, cmd, args, flags)
	}

	refresh, err := kubeconform.NewSchemaRefresh()
	if err != nil {
		return fmt.Errorf("prepare schema refresh: %w", err)
	}
	// Normal paths report cleanup errors explicitly; the defer also covers a panic.
	defer func() { _ = refresh.Close() }()

	flags.schemaRefresh = refresh

	err = runValidateCmdWithSchemaCache(ctx, cmd, args, flags)
	if err == nil {
		err = refresh.Commit()
	}

	return errors.Join(err, refresh.Close())
}
