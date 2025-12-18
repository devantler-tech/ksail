package installer

import "context"

// Installer defines methods for installing and uninstalling components.
type Installer interface {
	// Install installs the component.
	Install(ctx context.Context) error

	// Uninstall uninstalls the component.
	Uninstall(ctx context.Context) error
}
