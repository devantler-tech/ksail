package image

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

type nodeExportSource struct {
	node   string
	images []string
	wanted []string
}

type nodeImageInventory struct {
	node   string
	images []string
	lookup localImageLookup
}

// Preserve the direct single-node path; multi-node exports need a captured
// inventory and an assignment that covers every observed or requested name.
func (e *Exporter) findExportSources(
	ctx context.Context, cluster string, distribution v1alpha1.Distribution, requested []string,
) ([]nodeExportSource, error) {
	nodes, err := listClusterNodes(ctx, e.dockerClient, cluster, distribution)
	if err != nil {
		return nil, err
	}

	candidates := orderExportCandidates(nodes)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: no suitable node found in cluster %s", ErrNoNodes, cluster)
	}

	if len(candidates) == 1 {
		return []nodeExportSource{{node: candidates[0]}}, nil
	}

	requested = exportReferences(nil, requested)

	inventories, inventoryErr := e.readExportInventories(ctx, candidates, requested)
	if inventoryErr != nil && (len(requested) == 0 || ctx.Err() != nil) {
		return nil, fmt.Errorf(
			"%w: incomplete node image inventory: %w",
			ErrIncompleteExport,
			inventoryErr,
		)
	}

	wanted := exportReferences(inventories, requested)
	if len(wanted) == 0 {
		return nil, ErrNoImagesFound
	}

	sources, missing := planExportSources(inventories, wanted)
	if len(missing) != 0 {
		return nil, errors.Join(
			fmt.Errorf(
				"%w: unavailable references: %s",
				ErrIncompleteExport,
				strings.Join(missing, ", "),
			),
			inventoryErr,
		)
	}

	return sources, nil
}

func (e *Exporter) readExportInventories(
	ctx context.Context, candidates, requested []string,
) ([]nodeImageInventory, error) {
	var (
		inventories []nodeImageInventory
		failures    []error
	)

	for _, node := range candidates {
		refs, err := e.listImagesInNode(ctx, node)
		if err != nil {
			failures = append(failures, err)

			if len(requested) == 0 || ctx.Err() != nil {
				break
			}

			continue
		}

		inventory := nodeImageInventory{node: node, images: refs, lookup: newLocalImageLookup(refs)}

		inventories = append(inventories, inventory)
		if len(requested) > 0 && countHeldImages(inventory.lookup, requested) == len(requested) {
			break
		}
	}

	return inventories, errors.Join(failures...)
}

func exportReferences(inventories []nodeImageInventory, requested []string) []string {
	refs := slices.Clone(requested)
	if len(refs) == 0 {
		for _, inventory := range inventories {
			refs = append(refs, inventory.images...)
		}
	}

	seen := make(map[string]bool, len(refs))

	unique := make([]string, 0, len(refs))
	for _, ref := range normalizeImageRefs(refs) {
		if !seen[ref] {
			unique = append(unique, ref)
			seen[ref] = true
		}
	}

	return unique
}

// Prefer the node covering most remaining names, preserving preferred-role
// ties and the existing one-node optimization whenever it covers the request.
func planExportSources(
	inventories []nodeImageInventory,
	wanted []string,
) ([]nodeExportSource, []string) {
	remaining := slices.Clone(wanted)

	var sources []nodeExportSource

	for len(remaining) > 0 {
		var best nodeExportSource

		for _, inventory := range inventories {
			candidate := sourceForReferences(inventory, remaining)
			if len(candidate.wanted) > len(best.wanted) {
				best = candidate
			}
		}

		if len(best.wanted) == 0 {
			return nil, remaining
		}

		sources = append(sources, best)

		covered := make(map[string]bool, len(best.wanted))
		for _, ref := range best.wanted {
			covered[ref] = true
		}

		remaining = slices.DeleteFunc(remaining, func(ref string) bool { return covered[ref] })
	}

	return sources, nil
}

func sourceForReferences(inventory nodeImageInventory, wanted []string) nodeExportSource {
	source := nodeExportSource{node: inventory.node}
	seen := make(map[string]bool)

	for _, ref := range wanted {
		resolved := inventory.lookup.resolve(ref)
		if _, found := inventory.lookup.byExactRef[resolved]; !found {
			continue
		}

		source.wanted = append(source.wanted, ref)
		if !seen[resolved] {
			source.images = append(source.images, resolved)
			seen[resolved] = true
		}
	}

	return source
}
