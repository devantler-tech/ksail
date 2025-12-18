package io

// File permissions.
const (
	// filePermUserRW specifies user read/write permission.
	filePermUserRW = 0o600

	// dirPermUserGroupRX specifies directory permissions: user read/write/execute, group read/execute.
	dirPermUserGroupRX = 0o750
)
