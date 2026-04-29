package clusterautoscalerinstaller

import (
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
)

const (
	repoName  = "autoscaler"
	repoURL   = "https://kubernetes.github.io/autoscaler"
	namespace = "kube-system"

	defaultScaleDownUnneededTime = "10m"
)

// Installer installs or upgrades the Kubernetes Cluster Autoscaler.
//
// It embeds [helmutil.Base] to provide standard Helm chart lifecycle management
// (repository registration, install/upgrade, uninstall, image listing).
//
// The autoscaler runs on control-plane nodes and communicates with Hetzner Cloud
// via the pre-existing "hcloud" secret (created by hcloud-ccm) and the
// "cluster-autoscaler-config" secret (created by the Talos provisioner).
type Installer struct {
	*helmutil.Base
}

// NewInstaller creates a new Cluster Autoscaler installer instance.
// The nodeAutoscaler parameter drives the Helm values rendered for the chart:
// node pools, expander strategy, scale-down timing, and node count limits.
func NewInstaller(
	client helm.Interface,
	timeout time.Duration,
	nodeAutoscaler v1alpha1.NodeAutoscalerConfig,
) *Installer {
	return &Installer{
		Base: helmutil.NewBase(
			"cluster-autoscaler",
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: repoName,
				URL:  repoURL,
			},
			&helm.ChartSpec{
				ReleaseName: "cluster-autoscaler",
				ChartName:   "autoscaler/cluster-autoscaler",
				Namespace:   namespace,
				Version:     chartVersion(),
				RepoURL:     repoURL,
				Atomic:      true,
				Wait:        true,
				WaitForJobs: true,
				Timeout:     timeout,
				ValuesYaml:  buildValuesYaml(nodeAutoscaler),
			},
		),
	}
}

// buildValuesYaml generates the Helm values YAML for the cluster-autoscaler chart
// from the given NodeAutoscalerConfig.
func buildValuesYaml(cfg v1alpha1.NodeAutoscalerConfig) string {
	var buf strings.Builder

	fmt.Fprintln(&buf, "cloudProvider: hetzner")

	writeAutoscalingGroups(&buf, cfg.Pools)
	writeExtraArgs(&buf, cfg)
	writeExtraEnvSecrets(&buf)
	writeTolerations(&buf)
	writeNodeSelector(&buf)
	writeRBAC(&buf)
	writeResources(&buf)

	return buf.String()
}

// writeAutoscalingGroups writes the autoscalingGroups section from the node pools.
func writeAutoscalingGroups(buf *strings.Builder, pools []v1alpha1.NodePool) {
	if len(pools) == 0 {
		return
	}

	fmt.Fprintln(buf, "autoscalingGroups:")

	for _, pool := range pools {
		fmt.Fprintf(buf, "  - name: %s\n", pool.Name)
		fmt.Fprintf(buf, "    minSize: %d\n", pool.Min)
		fmt.Fprintf(buf, "    maxSize: %d\n", pool.Max)
		fmt.Fprintf(buf, "    instanceType: %s\n", pool.ServerType)
		fmt.Fprintf(buf, "    region: %s\n", pool.Location)
	}
}

// writeExtraArgs writes the extraArgs section with autoscaler tuning parameters.
func writeExtraArgs(buf *strings.Builder, cfg v1alpha1.NodeAutoscalerConfig) {
	fmt.Fprintln(buf, "extraArgs:")
	fmt.Fprintf(buf, "  expander: %s\n", expanderToHelmValue(cfg.Expander))

	scaleDownTime := cfg.ScaleDownUnneededTime
	if scaleDownTime == "" {
		scaleDownTime = defaultScaleDownUnneededTime
	}

	fmt.Fprintf(buf, "  scale-down-unneeded-time: %s\n", scaleDownTime)

	if cfg.MaxNodesTotal > 0 {
		fmt.Fprintf(buf, "  max-nodes-total: %d\n", cfg.MaxNodesTotal)
	}

	fmt.Fprintln(buf, "  scale-down-delay-after-add: 5m")
	fmt.Fprintln(buf, "  scale-down-delay-after-delete: 2m")
	fmt.Fprintln(buf, "  ok-total-unready-count: 3")
	fmt.Fprintln(buf, `  v: "4"`)
}

// writeExtraEnvSecrets writes the extraEnvSecrets section referencing the
// pre-existing "hcloud" and "cluster-autoscaler-config" Kubernetes secrets.
func writeExtraEnvSecrets(buf *strings.Builder) {
	fmt.Fprintln(buf, "extraEnvSecrets:")
	fmt.Fprintln(buf, "  HCLOUD_TOKEN:")
	fmt.Fprintln(buf, "    name: hcloud")
	fmt.Fprintln(buf, "    key: token")
	fmt.Fprintln(buf, "  HCLOUD_NETWORK:")
	fmt.Fprintln(buf, "    name: hcloud")
	fmt.Fprintln(buf, "    key: network")
	fmt.Fprintln(buf, "  HCLOUD_IMAGE:")
	fmt.Fprintln(buf, "    name: cluster-autoscaler-config")
	fmt.Fprintln(buf, "    key: hcloud_image")
	fmt.Fprintln(buf, "  HCLOUD_CLOUD_INIT:")
	fmt.Fprintln(buf, "    name: cluster-autoscaler-config")
	fmt.Fprintln(buf, "    key: hcloud_cloud_init")
}

// writeTolerations writes the tolerations section to schedule the autoscaler
// on control-plane nodes.
func writeTolerations(buf *strings.Builder) {
	fmt.Fprintln(buf, "tolerations:")
	fmt.Fprintln(buf, "  - key: node-role.kubernetes.io/control-plane")
	fmt.Fprintln(buf, "    operator: Exists")
	fmt.Fprintln(buf, "    effect: NoSchedule")
}

// writeNodeSelector writes the nodeSelector section to pin the autoscaler
// to control-plane nodes.
func writeNodeSelector(buf *strings.Builder) {
	fmt.Fprintln(buf, "nodeSelector:")
	fmt.Fprintln(buf, `  node-role.kubernetes.io/control-plane: ""`)
}

// writeRBAC writes the rbac section enabling RBAC resources and a dedicated
// service account.
func writeRBAC(buf *strings.Builder) {
	fmt.Fprintln(buf, "rbac:")
	fmt.Fprintln(buf, "  create: true")
	fmt.Fprintln(buf, "  serviceAccount:")
	fmt.Fprintln(buf, "    create: true")
	fmt.Fprintln(buf, "    name: cluster-autoscaler")
}

// writeResources writes the resources section with conservative CPU/memory
// requests and limits for the autoscaler pod.
func writeResources(buf *strings.Builder) {
	fmt.Fprintln(buf, "resources:")
	fmt.Fprintln(buf, "  requests:")
	fmt.Fprintln(buf, "    cpu: 50m")
	fmt.Fprintln(buf, "    memory: 128Mi")
	fmt.Fprintln(buf, "  limits:")
	fmt.Fprintln(buf, "    memory: 256Mi")
}

// expanderToHelmValue converts an [v1alpha1.AutoscalerExpander] enum value to
// the lowercase kebab-case string expected by the cluster-autoscaler Helm chart.
func expanderToHelmValue(expander v1alpha1.AutoscalerExpander) string {
	switch expander {
	case v1alpha1.AutoscalerExpanderPrice:
		return "price"
	case v1alpha1.AutoscalerExpanderLeastNodes:
		return "least-nodes"
	case v1alpha1.AutoscalerExpanderRandom:
		return "random"
	case v1alpha1.AutoscalerExpanderLeastWaste:
		fallthrough
	default:
		return "least-waste"
	}
}
