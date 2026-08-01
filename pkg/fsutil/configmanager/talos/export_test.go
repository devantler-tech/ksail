package talos

import talosconfig "github.com/siderolabs/talos/pkg/machinery/config"

// MigrateKubernetesPatchesForContract exposes migrateKubernetesPatchesForContract for
// tests so the migration can be asserted directly, independent of Talos config
// interpretation.
func MigrateKubernetesPatchesForContract(
	patches []Patch,
	versionContract *talosconfig.VersionContract,
) ([]Patch, error) {
	return migrateKubernetesPatchesForContract(patches, versionContract)
}
