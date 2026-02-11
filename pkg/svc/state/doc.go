// Package state provides cluster state persistence for distributions that
// cannot introspect their running configuration (Kind, K3d).
//
// State is stored as JSON in ~/.ksail/clusters/<name>/spec.json so that the
// update command can compare the desired configuration against the actual
// configuration used at creation time, avoiding false-positive diffs.
package state
