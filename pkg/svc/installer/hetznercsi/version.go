package hetznercsiinstaller

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image/parser"
)

//go:embed Chart.yaml
var chartYAML string

// chartVersion returns the pinned Hetzner CSI chart version extracted from the embedded
// Chart.yaml (kept in sync by Dependabot's helm ecosystem). The chart version diverges
// from the CSI driver image version, so it cannot be tracked via a Dockerfile image tag.
func chartVersion() string {
	return parser.ParseChartVersionFromChartYaml(chartYAML, "hcloud-csi")
}
