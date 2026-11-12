package image

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// localImageMode detects the node's supported image operation without guessing
// after failed or incomplete discovery. Older ctr versions already run locally.
func (e *ContainerExecutor) localImageMode(
	ctx context.Context, nodeName, operation string,
) (bool, error) {
	help, err := e.ExecInContainer(ctx, nodeName,
		[]string{ctrCommand, ctrNamespaceArg, ctrImages, operation, "--help"})
	if err != nil {
		return false, fmt.Errorf("failed to discover image %s options: %w", operation, err)
	}

	options := strings.Fields(help)
	if !slices.Contains(options, "--help,") && !slices.Contains(options, "--help") {
		return false, fmt.Errorf(
			"failed to discover image %s options: %w",
			operation,
			errIncompleteImageHelp,
		)
	}

	return slices.Contains(options, "--local"), nil
}
