// Package repairer defines the contract for repair operations on local
// KSail/Talos state files (talosconfig, kubeconfig, state files, ...).
//
// Each repair satisfies the [Repair] interface and reports its outcome
// as a [Result]. The `ksail cluster repair` command runs a plain slice
// of repairs — the standard set is returned by the talosconfig
// subpackage's DefaultRepairs — printing one status line per repair.
//
// Repairs MUST be idempotent and MUST back up files they modify.
package repairer
