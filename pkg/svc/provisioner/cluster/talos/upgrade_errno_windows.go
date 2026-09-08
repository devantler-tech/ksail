package talosprovisioner

import "golang.org/x/sys/windows"

// Windows sockets return a Winsock error, not syscall.ECONNREFUSED.
const errKubernetesUpgradeConnectionRefused = windows.WSAECONNREFUSED
