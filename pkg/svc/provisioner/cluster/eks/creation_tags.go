package eksprovisioner

import "fmt"

const (
	// Reserve the three eksctl stack identifiers and generated resource Name tag,
	// including when a particular external role/template would use fewer slots.
	nodegroupCreationReservedTagSlots = 4
	nodegroupCreationTagLimit         = 50
)

// validateCreationTagBudget bounds the supported experimental creation payload.
// eksctl merges metadata into node-group tags, then appends metadata and group
// tags separately to the CloudFormation tag array. Counting metadata twice is
// intentional; an extra slot also reserves generated resource Name tags.
func (p *nodegroupCreationPlan) validateCreationTagBudget(name string) error {
	metadata, _ := p.config["metadata"].(map[string]any)

	metadataTags, valid := metadata["tags"].(map[string]any)
	if metadata["tags"] != nil && !valid {
		return fmt.Errorf("%w: metadata.tags must be a mapping", errInvalidNodegroupConfig)
	}

	groupTags, _ := p.groups[name]["tags"].(map[string]any)
	keys := map[string]struct{}{
		nodegroupCreationTag:             {},
		"alpha.eksctl.io/nodegroup-name": {},
		"alpha.eksctl.io/nodegroup-type": {},
	}

	for _, tags := range []map[string]any{metadataTags, groupTags} {
		for key := range tags {
			keys[key] = struct{}{}
		}
	}

	slots := len(metadataTags) + len(keys) + nodegroupCreationReservedTagSlots
	if slots > nodegroupCreationTagLimit {
		return fmt.Errorf(
			"%w: nodegroup %q exceeds the creation tag budget: %d slots, maximum %d "+
				"including metadata, KSail and eksctl reserved tags",
			errInvalidNodegroupConfig, name, slots, nodegroupCreationTagLimit,
		)
	}

	return nil
}
