package talosprovisioner

import "github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"

// HetznerBootSource exposes hetznerBootSource for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var HetznerBootSource = hetznerBootSource

// HetznerScaleServerOptsForTest exposes hetznerScaleServerOpts for tests.
func (p *Provisioner) HetznerScaleServerOptsForTest(
	clusterName, role, nodeName string,
	nodeNumber int,
	infra HetznerInfra,
	imageID int64,
) hetzner.CreateServerOpts {
	return p.hetznerScaleServerOpts(clusterName, role, nodeName, nodeNumber, infra, imageID)
}

// IsRetryableTalosApplyConfigError exposes the unexported helper for tests in
// the talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var IsRetryableTalosApplyConfigError = isRetryableTalosApplyConfigError

// PatchTalosHostname exposes patchTalosHostname for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var PatchTalosHostname = patchTalosHostname

// UserHostnameConfigSummary exposes userHostnameConfigSummary for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var UserHostnameConfigSummary = userHostnameConfigSummary

// WarnIfOverridingUserHostnameForTest exposes warnIfOverridingUserHostname for
// tests in the talosprovisioner_test package.
func (p *Provisioner) WarnIfOverridingUserHostnameForTest(cfgBytes []byte) {
	p.warnIfOverridingUserHostname(cfgBytes)
}

// HetznerNodeName exposes hetznerNodeName for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var HetznerNodeName = hetznerNodeName

// HetznerNodeTalosAddress exposes hetznerNodeTalosAddress for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var HetznerNodeTalosAddress = hetznerNodeTalosAddress

// DiagnoseUnreachableNode exposes diagnoseUnreachableNode for tests in the
// talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var DiagnoseUnreachableNode = diagnoseUnreachableNode

// MaxNodeNameLength exposes maxNodeNameLength for tests.
const MaxNodeNameLength = maxNodeNameLength
