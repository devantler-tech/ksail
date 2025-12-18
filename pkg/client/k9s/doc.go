// Package k9s provides a K9s client implementation.
//
// This package wraps the K9s terminal UI application and provides an executor
// interface for launching K9s sessions connected to Kubernetes clusters.
//
// Coverage Note: The DefaultK9sExecutor.Execute() method and parts of the
// HandleConnectRunE execution path cannot be fully tested in unit tests because they
// require launching k9s which needs an actual terminal UI. These paths are validated
// through integration testing with actual k9s installation and manual verification.
package k9s
