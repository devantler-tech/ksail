package installer

import "context"

// Installer defines methods for installing and uninstalling components.
type Installer interface {
	// Install installs the component.
	Install(ctx context.Context) error

	// Uninstall uninstalls the component.
	Uninstall(ctx context.Context) error

	// Images returns the container images used by this component.
	// The images are extracted from the rendered Helm chart manifests.
	Images(ctx context.Context) ([]string, error)
}
