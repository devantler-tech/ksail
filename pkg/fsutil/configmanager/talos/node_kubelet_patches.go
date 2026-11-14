package talos

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configpatcher"
	"github.com/siderolabs/talos/pkg/machinery/config/generate"
	"sigs.k8s.io/yaml"
)

const (
	kubeNodeConfigKind    = "KubeNodeConfig"
	kubeletConfigKind     = "KubeletConfig"
	kubeClusterConfigKind = "KubeClusterConfig"
)

var (
	// ErrLegacyKubeletFieldUnsupported marks a legacy machine.kubelet field that has no Talos 1.14
	// document equivalent. Its message names the fields by key only, never by value.
	ErrLegacyKubeletFieldUnsupported = errors.New(
		"kubelet field has no Talos 1.14 KubeletConfig or KubeNodeConfig equivalent",
	)
	errLegacyAllowSchedulingType = errors.New("expected true or false")
)

const (
	kubeletImageField     = "image"
	kubeletExtraArgsField = "extraArgs"
)

// legacyNodeFields maps the v1alpha1 machine fields that Talos 1.14 moves into
// the KubeNodeConfig document to their name in that document.
func legacyNodeFields() map[string]string {
	return map[string]string{
		"nodeLabels":      "labels",
		"nodeAnnotations": "annotations",
		"nodeTaints":      "taints",
	}
}

// legacyKubeletNodeFields maps the v1alpha1 machine.kubelet fields that Talos
// 1.14 moves into the KubeNodeConfig document.
func legacyKubeletNodeFields() map[string]string {
	return map[string]string{
		"registerWithFQDN":     "registerWithFQDN",
		"nodeIP":               "nodeIP",
		"skipNodeRegistration": "skipNodeRegistration",
	}
}

// legacyKubeletFields maps the v1alpha1 machine.kubelet fields that Talos 1.14
// moves into the KubeletConfig document.
func legacyKubeletFields() map[string]string {
	return map[string]string{
		kubeletImageField:                     kubeletImageField,
		"clusterDNS":                          "clusterDNS",
		kubeletExtraArgsField:                 kubeletExtraArgsField,
		"extraConfig":                         "config",
		"defaultRuntimeSeccompProfileEnabled": "defaultRuntimeSeccompProfileEnabled",
	}
}

// nodeKubeletMigration is the result of moving legacy node and kubelet fields
// out of a patch set.
type nodeKubeletMigration struct {
	patches []Patch
	// allowSchedulingOnControlPlanes is the value a control-plane node sees, or
	// nil when no cluster or control-plane patch sets it.
	allowSchedulingOnControlPlanes *bool
}

// controlPlaneScheduling resolves allowSchedulingOnControlPlanes the way Talos
// patches a control-plane node: every cluster patch first, then every
// control-plane patch, each in input order. Worker patches never reach a
// control plane, and the setting only taints control planes, so a worker
// patch's value is dropped with the field.
type controlPlaneScheduling struct {
	cluster      *bool
	controlPlane *bool
}

func (s *controlPlaneScheduling) record(scope PatchScope, value *bool) {
	if value == nil {
		return
	}

	switch scope {
	case PatchScopeCluster:
		s.cluster = value
	case PatchScopeControlPlane:
		s.controlPlane = value
	case PatchScopeWorker:
	}
}

func (s *controlPlaneScheduling) resolve() *bool {
	if s.controlPlane != nil {
		return s.controlPlane
	}

	return s.cluster
}

// MigratePatchesToMultiDocument moves the v1alpha1 fields that the Talos 1.14
// documents own out of patches applied outside this manager, such as
// autoscaler pool patches, with the same migrations the manager applies to its
// own patches. Without it, a patch that sets machine.nodeLabels on a 1.14
// config leaves both the legacy field and KubeNodeConfig in place, and Talos
// rejects the result.
//
// A patch's cluster.allowSchedulingOnControlPlanes only decides the generated
// control-plane taint, so it has no effect on a config that is already
// generated and is dropped.
func MigratePatchesToMultiDocument(patches []Patch) ([]Patch, error) {
	migrated, err := migrateKubernetesPatchesForContract(patches, talosconfig.TalosVersion1_14)
	if err != nil {
		return nil, fmt.Errorf("migrate Kubernetes patches: %w", err)
	}

	migration, err := migrateNodeAndKubeletPatchesForContract(
		migrated,
		talosconfig.TalosVersion1_14,
	)
	if err != nil {
		return nil, fmt.Errorf("migrate node and kubelet patches: %w", err)
	}

	return migration.patches, nil
}

// migrateNodeAndKubeletPatchesForContract moves the v1alpha1 node, kubelet and
// cluster-name fields that Talos 1.14 hands to the KubeNodeConfig,
// KubeletConfig and KubeClusterConfig documents. Under that contract Talos
// refuses a config that still sets them in v1alpha1, and drops what it does not refuse (a cluster patch's
// machine.nodeLabels never reaches the node).
//
// cluster.allowSchedulingOnControlPlanes is lifted out of the patches rather
// than translated: in 1.14 it decides whether the generated control-plane
// KubeNodeConfig carries the NoSchedule taint, so it is a generation option.
//
// Earlier contracts keep the v1alpha1 fields, so their patches pass through
// unchanged. The input slice is never modified, so regenerating from the
// stored patches migrates them again.
func migrateNodeAndKubeletPatchesForContract(
	patches []Patch,
	versionContract *talosconfig.VersionContract,
) (nodeKubeletMigration, error) {
	if versionContract == nil || !versionContract.MultidocKubernetesConfigSupported() {
		return nodeKubeletMigration{patches: patches}, nil
	}

	result := nodeKubeletMigration{patches: make([]Patch, 0, len(patches))}

	var scheduling controlPlaneScheduling

	for _, patch := range patches {
		migrated, allowScheduling, err := migrateNodeAndKubeletPatch(patch)
		if err != nil {
			return nodeKubeletMigration{}, err
		}

		scheduling.record(patch.Scope, allowScheduling)

		result.patches = append(result.patches, migrated...)
	}

	result.allowSchedulingOnControlPlanes = scheduling.resolve()

	return result, nil
}

// migrateNodeAndKubeletPatch returns the patch, split into the remaining
// legacy content plus one patch per new document so no single patch carries
// two documents of the same kind.
func migrateNodeAndKubeletPatch(patch Patch) ([]Patch, *bool, error) {
	documents, mapDocuments, err := decodeLegacyKubernetesDocuments(patch)
	if err != nil {
		return nil, nil, err
	}

	if !mapDocuments {
		return []Patch{patch}, nil, nil
	}

	var (
		allowScheduling *bool
		newDocuments    []map[string]any
		changed         bool
	)

	for _, document := range documents {
		allow, moved, documentChanged, migrateErr := migrateNodeAndKubeletDocument(
			document,
			patch.Path,
		)
		if migrateErr != nil {
			return nil, nil, migrateErr
		}

		if allow != nil {
			allowScheduling = allow
		}

		newDocuments = append(newDocuments, moved...)
		changed = changed || documentChanged
	}

	if !changed {
		return []Patch{patch}, nil, nil
	}

	migrated, err := splitMigratedNodeKubeletPatch(patch, documents, newDocuments)
	if err != nil {
		return nil, nil, err
	}

	return migrated, allowScheduling, nil
}

func popAllowScheduling(document map[string]any, patchPath string) (bool, bool, error) {
	cluster, found := mapValue(document, "cluster")
	if !found {
		return false, false, nil
	}

	var (
		allow bool
		set   bool
	)

	// allowSchedulingOnMasters is the pre-1.5 spelling of the same setting.
	for _, field := range []string{"allowSchedulingOnMasters", "allowSchedulingOnControlPlanes"} {
		value, present := cluster[field]
		if !present {
			continue
		}

		enabled, isBool := value.(bool)
		if !isBool {
			return false, false, fmt.Errorf(
				"patch %q: cluster.%s: %w",
				patchPath,
				field,
				errLegacyAllowSchedulingType,
			)
		}

		delete(cluster, field)

		allow = enabled
		set = true
	}

	removeEmptyMap(document, "cluster", cluster)

	return allow, set, nil
}

// moveLegacyMachineFields removes the node and kubelet fields from a v1alpha1
// document and returns them as KubeNodeConfig and KubeletConfig documents (nil
// when there is nothing to move).
func moveLegacyMachineFields(
	document map[string]any,
	patchPath string,
) (map[string]any, map[string]any, bool, error) {
	machine, found := mapValue(document, "machine")
	if !found {
		return nil, nil, false, nil
	}

	nodeDocument := map[string]any{}
	kubeletDocument := map[string]any{}

	moveFields(machine, nodeDocument, legacyNodeFields())

	kubelet, hasKubelet := mapValue(machine, "kubelet")
	if hasKubelet {
		moveFields(kubelet, nodeDocument, legacyKubeletNodeFields())
		moveFields(kubelet, kubeletDocument, legacyKubeletFields())

		if len(kubelet) > 0 {
			remaining := slices.Sorted(maps.Keys(kubelet))

			return nil, nil, false, fmt.Errorf(
				"patch %q: machine.kubelet.%s: %w; move it into its own Talos 1.14 configuration document",
				patchPath,
				strings.Join(remaining, ", machine.kubelet."),
				ErrLegacyKubeletFieldUnsupported,
			)
		}

		delete(machine, "kubelet")
	}

	removeEmptyMap(document, "machine", machine)

	// Removing a legacy kubelet object is a change even when it was empty:
	// Talos rejects any machine.kubelet beside a KubeletConfig document.
	return withKind(
			nodeDocument,
			kubeNodeConfigKind,
		), withKind(
			kubeletDocument,
			kubeletConfigKind,
		), hasKubelet, nil
}

func moveFields(source, target map[string]any, fields map[string]string) {
	for legacy, current := range fields {
		value, found := source[legacy]
		if !found {
			continue
		}

		delete(source, legacy)

		target[current] = value
	}
}

func withKind(document map[string]any, kind string) map[string]any {
	if len(document) == 0 {
		return nil
	}

	document[apiVersionField] = multiDocumentAPIVersion
	document[kindField] = kind

	return document
}

func splitMigratedNodeKubeletPatch(
	patch Patch,
	legacyDocuments []map[string]any,
	newDocuments []map[string]any,
) ([]Patch, error) {
	// Re-encoding through a map sorts keys, and Talos selects a list entry by
	// its first non-$patch key, so a multi-key deletion could change target.
	for _, document := range legacyDocuments {
		if ambiguousListDeletion(document) {
			return nil, fmt.Errorf("migrate patch %q: %w", patch.Path, errMigrationDeletion)
		}
	}

	migrated := make([]Patch, 0, len(newDocuments))

	remaining, err := marshalNonEmptyDocuments(legacyDocuments)
	if err != nil {
		return nil, fmt.Errorf("marshal migrated patch %q: %w", patch.Path, err)
	}

	if len(remaining) > 0 {
		legacy := patch

		legacy.Content = append(bytes.Join(remaining, []byte("\n---\n")), '\n')
		migrated = append(migrated, legacy)
	}

	for _, document := range newDocuments {
		encoded, err := yaml.Marshal(document)
		if err != nil {
			return nil, fmt.Errorf("marshal migrated patch %q: %w", patch.Path, err)
		}

		moved := patch
		moved.Content = encoded
		migrated = append(migrated, moved)
	}

	return migrated, nil
}

// moveLegacyClusterName moves cluster.clusterName into a KubeClusterConfig
// document, which owns the name under the 1.14 contract.
func moveLegacyClusterName(document map[string]any) map[string]any {
	cluster, found := mapValue(document, "cluster")
	if !found {
		return nil
	}

	clusterDocument := map[string]any{}
	moveFields(cluster, clusterDocument, map[string]string{"clusterName": "clusterName"})
	removeEmptyMap(document, "cluster", cluster)

	return withKind(clusterDocument, kubeClusterConfigKind)
}

// migrateNodeAndKubeletDocument migrates one v1alpha1 document in place. It
// returns the allowSchedulingOnControlPlanes value it lifted (nil when absent),
// the new documents, and whether the document changed.
func migrateNodeAndKubeletDocument(
	document map[string]any,
	patchPath string,
) (*bool, []map[string]any, bool, error) {
	if _, multiDocument := document[kindField]; multiDocument {
		return nil, nil, false, nil
	}

	var allowScheduling *bool

	allow, found, err := popAllowScheduling(document, patchPath)
	if err != nil {
		return nil, nil, false, err
	}

	if found {
		allowScheduling = &allow
	}

	nodeDocument, kubeletDocument, kubeletRemoved, err := moveLegacyMachineFields(
		document,
		patchPath,
	)
	if err != nil {
		return nil, nil, false, err
	}

	candidates := []map[string]any{
		moveLegacyClusterName(document), nodeDocument, kubeletDocument,
	}
	moved := make([]map[string]any, 0, len(candidates))

	for _, candidate := range candidates {
		if candidate != nil {
			moved = append(moved, candidate)
		}
	}

	return allowScheduling, moved, found || kubeletRemoved || len(moved) > 0, nil
}

// genOptions returns the generation options the migration lifted out of the
// patches.
func (m nodeKubeletMigration) genOptions() []generate.Option {
	if m.allowSchedulingOnControlPlanes == nil {
		return nil
	}

	return []generate.Option{
		generate.WithAllowSchedulingOnControlPlanes(*m.allowSchedulingOnControlPlanes),
	}
}

// scopedPatches are a contract's migrated patches split by scope, with the
// generation options the migration lifted out of them.
type scopedPatches struct {
	cluster      []configpatcher.Patch
	controlPlane []configpatcher.Patch
	worker       []configpatcher.Patch
	genOptions   []generate.Option
}

// scopePatchesForContract migrates the node and kubelet fields for the
// contract and splits the result by scope.
func scopePatchesForContract(
	patches []Patch,
	versionContract *talosconfig.VersionContract,
) (scopedPatches, error) {
	migration, err := migrateNodeAndKubeletPatchesForContract(patches, versionContract)
	if err != nil {
		return scopedPatches{}, fmt.Errorf("failed to migrate node and kubelet patches: %w", err)
	}

	cluster, controlPlane, worker, err := categorizePatchesByScope(migration.patches)
	if err != nil {
		return scopedPatches{}, err
	}

	return scopedPatches{
		cluster:      cluster,
		controlPlane: controlPlane,
		worker:       worker,
		genOptions:   migration.genOptions(),
	}, nil
}
