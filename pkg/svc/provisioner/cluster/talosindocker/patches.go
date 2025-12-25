package talosindockerprovisioner

import (
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
)

// PatchScope indicates which nodes a Talos patch applies to.
// This is an alias to talosconfigmanager.PatchScope for backwards compatibility.
type PatchScope = talosconfigmanager.PatchScope

// PatchScope constants for backwards compatibility.
const (
	// PatchScopeCluster applies the patch to all nodes.
	PatchScopeCluster = talosconfigmanager.PatchScopeCluster
	// PatchScopeControlPlane applies the patch to control-plane nodes only.
	PatchScopeControlPlane = talosconfigmanager.PatchScopeControlPlane
	// PatchScopeWorker applies the patch to worker nodes only.
	PatchScopeWorker = talosconfigmanager.PatchScopeWorker
)

// TalosPatch represents a Talos machine configuration patch.
// This is an alias to talosconfigmanager.Patch for backwards compatibility.
type TalosPatch = talosconfigmanager.Patch
