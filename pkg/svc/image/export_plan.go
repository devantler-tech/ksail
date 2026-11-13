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

// exportPlan is the node assignment for an export, together with what a later
// re-plan needs: every candidate node and the inventories read so far. A
// multi-node export uses it to move an image to another holder when the node it
// was assigned to cannot export it.
type exportPlan struct {
	sources     []nodeExportSource
	candidates  []string
	inventories []nodeImageInventory
	// inventoried names every candidate whose inventory was read or tried.
	inventoried map[string]bool
}

// Preserve the direct single-node path; multi-node exports need a captured
// inventory and an assignment that covers every observed or requested name.
func (e *Exporter) findExportSources(
	ctx context.Context, cluster string, distribution v1alpha1.Distribution, requested []string,
) (exportPlan, error) {
	nodes, err := listClusterNodes(ctx, e.dockerClient, cluster, distribution)
	if err != nil {
		return exportPlan{}, err
	}

	candidates := orderExportCandidates(nodes)
	if len(candidates) == 0 {
		return exportPlan{}, fmt.Errorf(
			"%w: no suitable node found in cluster %s",
			ErrNoNodes,
			cluster,
		)
	}

	if len(candidates) == 1 {
		return exportPlan{sources: []nodeExportSource{{node: candidates[0]}}}, nil
	}

	requested = exportReferences(nil, requested)
	plan := exportPlan{candidates: candidates, inventoried: make(map[string]bool, len(candidates))}

	inventoryErr := e.readExportInventories(ctx, &plan, requested)
	if inventoryErr != nil && (len(requested) == 0 || ctx.Err() != nil) {
		return exportPlan{}, fmt.Errorf(
			"%w: incomplete node image inventory: %w",
			ErrIncompleteExport,
			inventoryErr,
		)
	}

	wanted := exportReferences(plan.inventories, requested)
	if len(wanted) == 0 {
		return exportPlan{}, ErrNoImagesFound
	}

	sources, missing := planExportSources(plan.inventories, wanted)
	if len(missing) != 0 {
		return exportPlan{}, errors.Join(
			fmt.Errorf(
				"%w: unavailable references: %s",
				ErrIncompleteExport,
				strings.Join(missing, ", "),
			),
			inventoryErr,
		)
	}

	plan.sources = sources

	return plan, nil
}

// readExportInventories lists the images held by each candidate node the plan
// has not read yet, and adds them to the plan. With an explicit request it
// skips a node it cannot read and reports the failure; without one, or once the
// context ends, it stops at the first failure. It stops early once one node
// holds every requested image; a re-plan reads the remaining nodes later.
func (e *Exporter) readExportInventories(
	ctx context.Context, plan *exportPlan, requested []string,
) error {
	var failures []error

	for _, node := range plan.candidates {
		if plan.inventoried[node] {
			continue
		}

		plan.inventoried[node] = true

		refs, err := e.listImagesInNode(ctx, node)
		if err != nil {
			failures = append(failures, err)

			if len(requested) == 0 || ctx.Err() != nil {
				break
			}

			continue
		}

		inventory := nodeImageInventory{node: node, images: refs, lookup: newLocalImageLookup(refs)}

		plan.inventories = append(plan.inventories, inventory)
		if len(requested) > 0 && countHeldImages(inventory.lookup, requested) == len(requested) {
			break
		}
	}

	return errors.Join(failures...)
}

// replan assigns wanted references to nodes other than the excluded ones, after
// the node a share was assigned to could not export it. It first reads any
// candidate whose inventory the first plan skipped. It fails when some
// reference has no other holder, naming the references and any inventory
// failure.
func (e *Exporter) replan(
	ctx context.Context, plan *exportPlan, wanted []string, excluded map[string]bool,
) ([]nodeExportSource, error) {
	inventoryErr := e.readRemainingInventories(ctx, plan)

	usable := slices.DeleteFunc(
		slices.Clone(plan.inventories),
		func(inventory nodeImageInventory) bool {
			return excluded[inventory.node]
		},
	)

	sources, missing := planExportSources(usable, wanted)
	if len(missing) != 0 {
		return nil, errors.Join(
			fmt.Errorf(
				"%w: no other node holds: %s",
				ErrIncompleteExport,
				strings.Join(missing, ", "),
			),
			inventoryErr,
		)
	}

	return sources, nil
}

// readRemainingInventories reads every candidate whose inventory the plan has
// not read or tried yet, collecting the failures.
func (e *Exporter) readRemainingInventories(ctx context.Context, plan *exportPlan) error {
	var failures []error

	for _, node := range plan.candidates {
		if plan.inventoried[node] || ctx.Err() != nil {
			continue
		}

		plan.inventoried[node] = true

		refs, err := e.listImagesInNode(ctx, node)
		if err != nil {
			failures = append(failures, err)

			continue
		}

		plan.inventories = append(plan.inventories,
			nodeImageInventory{node: node, images: refs, lookup: newLocalImageLookup(refs)})
	}

	return errors.Join(failures...)
}

// exportReferences returns the normalized, de-duplicated references to export:
// the requested ones, or every image the inventories hold when none is requested.
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

// sourceForReferences reports which wanted references the node can serve and the
// distinct local image names it would export for them.
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
