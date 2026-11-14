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
	options, err := e.imageOptions(ctx, nodeName, operation)
	if err != nil {
		return false, err
	}

	return slices.Contains(options, "--local"), nil
}

// imageOptions reads the options the node's ctr advertises for one image operation. It fails
// instead of guessing when the help cannot be read or is incomplete.
func (e *ContainerExecutor) imageOptions(
	ctx context.Context, nodeName, operation string,
) ([]string, error) {
	help, err := e.ExecInContainer(ctx, nodeName,
		[]string{ctrCommand, ctrNamespaceArg, ctrImages, operation, "--help"})
	if err != nil {
		return nil, fmt.Errorf("failed to discover image %s options: %w", operation, err)
	}

	options := strings.Fields(help)
	if !slices.Contains(options, "--help,") && !slices.Contains(options, "--help") {
		return nil, fmt.Errorf(
			"failed to discover image %s options: %w",
			operation,
			errIncompleteImageHelp,
		)
	}

	return options, nil
}
