package cloudproviderkindinstaller

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image/parser"
)

//go:embed Dockerfile
var dockerfile string

// CloudProviderKindImage returns the cloud-provider-kind image.
func CloudProviderKindImage() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+(registry\.k8s\.io/cloud-provider-kind/[^\s]+)`,
		"cloud-provider-kind",
	)
}
