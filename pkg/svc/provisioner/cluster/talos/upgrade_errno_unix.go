//go:build !windows

package talosprovisioner

import "syscall"

const errKubernetesUpgradeConnectionRefused = syscall.ECONNREFUSED
