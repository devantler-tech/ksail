// Package repairer provides a generic registry for repair operations on
// local KSail/Talos state files (talosconfig, kubeconfig, state files,
// ...).
//
// Each repair satisfies the Repair interface and is registered in a
// [Registry] (typically via [(*Registry).Register] on [Default]()). The
// `ksail cluster repair`
// command iterates
// every registered repair and runs it.
//
// Repairs MUST be idempotent and MUST back up files they modify.
package repairer
