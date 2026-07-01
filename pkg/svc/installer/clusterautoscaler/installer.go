package clusterautoscalerinstaller

import (
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/helmutil"
	"sigs.k8s.io/yaml"
)

const (
	repoName  = "autoscaler"
	repoURL   = "https://kubernetes.github.io/autoscaler"
	namespace = "kube-system"

	defaultScaleDownUnneededTime = "10m"
	defaultOkTotalUnreadyCount   = 3

	// ReleaseName is the Helm release name for the cluster-autoscaler chart, which
	// the chart also stamps as the app.kubernetes.io/instance label value. The Talos
	// provisioner selects on that label to roll the Deployment when its config Secret
	// changes, so both derive from this single constant rather than drifting.
	ReleaseName = "cluster-autoscaler"

	// AutoscalerConfigSecretName is the Kubernetes Secret containing per-cluster
	// autoscaler parameters (cloud-init template, base image tag). It is created by
	// the Talos provisioner's ApplyAutoscalerConfigSecret before the installer runs.
	AutoscalerConfigSecretName = "cluster-autoscaler-config"

	// AutoscalerConfigHcloudClusterConfigKey is the key in AutoscalerConfigSecretName
	// that holds the Hetzner cluster-autoscaler's HCLOUD_CLUSTER_CONFIG value: a
	// base64-encoded JSON document carrying the snapshot image (imagesForArch) and
	// per-pool node configuration (nodeConfigs[<pool>] = {cloudInit, labels, taints}).
	// It replaces the legacy single-image / single-cloud-init keys (HCLOUD_IMAGE,
	// HCLOUD_CLOUD_INIT), enabling per-pool labels and taints. Each pool's cloudInit
	// is base64(gzip(workerConfig)) — used verbatim as the Hetzner user_data, which
	// the Talos hcloud platform base64-decodes and un-gzips before parsing, keeping
	// each payload under Hetzner's 32 KiB user_data limit (issue #5015). The value is
	// written by the Talos provisioner's ApplyAutoscalerConfigSecret.
	AutoscalerConfigHcloudClusterConfigKey = "hcloud_cluster_config"
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
// When haEnabled is true the chart is configured with replicas=2
// for fast failover via leader election.
// workerPublicIPv4/workerPublicIPv6 mirror the Hetzner worker public-net toggles;
// when either is false the autoscaler is configured (via HCLOUD_PUBLIC_IPV4/IPV6) so
// new nodes match the worker public-net setting. The Hetzner cluster-autoscaler only
// supports this cluster-wide, not per node pool.
func NewInstaller(
	client helm.Interface,
	timeout time.Duration,
	nodeAutoscaler v1alpha1.NodeAutoscalerConfig,
	haEnabled bool,
	workerPublicIPv4, workerPublicIPv6 bool,
) (*Installer, error) {
	extraEnv := hetznerPublicNetEnv(workerPublicIPv4, workerPublicIPv6)

	valuesYaml, err := buildValuesYaml(nodeAutoscaler, haEnabled, extraEnv)
	if err != nil {
		return nil, fmt.Errorf("clusterautoscaler: failed to build chart values: %w", err)
	}

	return &Installer{
		Base: helmutil.NewBase(
			ReleaseName,
			client,
			timeout,
			&helm.RepositoryEntry{
				Name: repoName,
				URL:  repoURL,
			},
			&helm.ChartSpec{
				ReleaseName: ReleaseName,
				ChartName:   "autoscaler/cluster-autoscaler",
				Namespace:   namespace,
				Version:     chartVersion(),
				RepoURL:     repoURL,
				Atomic:      true,
				Wait:        true,
				WaitForJobs: true,
				Timeout:     timeout,
				ValuesYaml:  valuesYaml,
			},
		),
	}, nil
}

// chartValues mirrors the cluster-autoscaler Helm chart values schema.
// All user-supplied strings are embedded in struct fields so that
// sigs.k8s.io/yaml handles escaping, preventing YAML injection.
type chartValues struct {
	Replicas          int32                `json:"replicas,omitempty"`
	CloudProvider     string               `json:"cloudProvider"`
	AutoscalingGroups []autoscalingGroup   `json:"autoscalingGroups,omitempty"`
	ExtraArgs         chartExtraArgs       `json:"extraArgs"`
	ExtraEnv          map[string]string    `json:"extraEnv,omitempty"`
	ExtraEnvSecrets   chartExtraEnvSecrets `json:"extraEnvSecrets"`
	Tolerations       []chartToleration    `json:"tolerations"`
	NodeSelector      map[string]string    `json:"nodeSelector"`
	RBAC              chartRBAC            `json:"rbac"`
	Resources         chartResources       `json:"resources"`
	// ExtraObjects maps to the chart's extraObjects value: raw Kubernetes manifests
	// rendered as part of the Helm release. Used to deliver the CapacityBuffer CRD
	// when capacity buffers are enabled (see capacitybuffers.go).
	ExtraObjects []map[string]any `json:"extraObjects,omitempty"`
}

type autoscalingGroup struct {
	Name         string `json:"name"`
	MinSize      int32  `json:"minSize"`
	MaxSize      int32  `json:"maxSize"`
	InstanceType string `json:"instanceType"`
	Region       string `json:"region"`
}

//nolint:tagliatelle // Helm chart requires kebab-case keys for these extraArgs.
type chartExtraArgs struct {
	Expander              string `json:"expander"`
	ScaleDownUnneededTime string `json:"scale-down-unneeded-time"`
	MaxNodesTotal         int32  `json:"max-nodes-total,omitempty"`
	ScaleDownAfterAdd     string `json:"scale-down-delay-after-add"`
	ScaleDownAfterDelete  string `json:"scale-down-delay-after-delete"`
	OkTotalUnreadyCount   int    `json:"ok-total-unready-count"`
	V                     string `json:"v"`
	// CapacityBufferControllerEnabled and CapacityBufferPodInjectionEnabled toggle
	// the upstream capacity-buffers feature (Cluster Autoscaler 1.34+, off by
	// default): the CapacityBuffer controller and the pod-list processor that
	// injects virtual pods for ready buffers. omitempty keeps both flags out of
	// the rendered values unless capacityBuffers is enabled, so existing releases
	// see no values drift.
	CapacityBufferControllerEnabled   bool `json:"capacity-buffer-controller-enabled,omitempty"`
	CapacityBufferPodInjectionEnabled bool `json:"capacity-buffer-pod-injection-enabled,omitempty"`
	// KubeAPIContentType forces the autoscaler's Kubernetes client to negotiate the
	// given content type. capacity-buffers require application/json: the CapacityBuffer
	// controller's client would otherwise negotiate protobuf (the autoscaler default for
	// built-in types), but CapacityBuffer is a CRD and cannot be protobuf-encoded, so the
	// controller fails ("does not implement the protobuf marshalling interface") and never
	// writes buffer status — silently disabling over-provisioning (ksail#5603). omitempty
	// keeps it out of the rendered values unless capacityBuffers is enabled, so existing
	// releases see no values drift.
	KubeAPIContentType string `json:"kube-api-content-type,omitempty"`
}

type chartSecretRef struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

//nolint:tagliatelle // Helm chart requires UPPER_SNAKE_CASE env var keys.
type chartExtraEnvSecrets struct {
	HcloudToken         chartSecretRef `json:"HCLOUD_TOKEN"`
	HcloudNetwork       chartSecretRef `json:"HCLOUD_NETWORK"`
	HcloudClusterConfig chartSecretRef `json:"HCLOUD_CLUSTER_CONFIG"`
}

type chartToleration struct {
	Key      string `json:"key"`
	Operator string `json:"operator"`
	Effect   string `json:"effect"`
}

type chartRBACServiceAccount struct {
	Create bool   `json:"create"`
	Name   string `json:"name"`
}

type chartRBAC struct {
	Create         bool                    `json:"create"`
	ServiceAccount chartRBACServiceAccount `json:"serviceAccount"`
	// AdditionalRules maps to the chart's rbac.additionalRules value: extra
	// ClusterRole rules appended to the chart-managed ClusterRole. Used to grant
	// CapacityBuffer access, which the chart's own ClusterRole does not cover.
	AdditionalRules []chartRBACRule `json:"additionalRules,omitempty"`
}

// chartRBACRule mirrors a single rbac.authorization.k8s.io PolicyRule entry in
// the chart's rbac.additionalRules value.
type chartRBACRule struct {
	APIGroups []string `json:"apiGroups"`
	Resources []string `json:"resources"`
	Verbs     []string `json:"verbs"`
}

type chartResourceRequests struct {
	CPU    string `json:"cpu,omitempty"`
	Memory string `json:"memory"`
}

type chartResourceLimits struct {
	Memory string `json:"memory"`
}

type chartResources struct {
	Requests chartResourceRequests `json:"requests"`
	Limits   chartResourceLimits   `json:"limits"`
}

// buildValuesYaml generates the Helm values YAML for the cluster-autoscaler chart
// from the given NodeAutoscalerConfig. It uses a typed struct marshaled via
// sigs.k8s.io/yaml to prevent YAML injection from user-supplied strings.
// When haEnabled is true an extra standby replica is configured.
func buildValuesYaml(
	cfg v1alpha1.NodeAutoscalerConfig,
	haEnabled bool,
	extraEnv map[string]string,
) (string, error) {
	vals := buildChartValues(cfg, haEnabled, extraEnv)

	if cfg.CapacityBuffers {
		err := enableCapacityBuffers(&vals)
		if err != nil {
			return "", fmt.Errorf("failed to enable capacity buffers: %w", err)
		}
	}

	out, err := yaml.Marshal(vals)
	if err != nil {
		return "", fmt.Errorf("failed to marshal Helm chart values: %w", err)
	}

	return string(out), nil
}

// hetznerPublicNetEnv builds the cluster-autoscaler extraEnv entries that disable
// public IPs on autoscaler-created nodes. The Hetzner cluster-autoscaler controls
// public IPs globally via HCLOUD_PUBLIC_IPV4 / HCLOUD_PUBLIC_IPV6 (not per node pool),
// so autoscaler nodes inherit the worker public-net setting cluster-wide. Only the
// disabling entries are emitted; unset means the autoscaler default (public IP) applies.
func hetznerPublicNetEnv(workerPublicIPv4, workerPublicIPv6 bool) map[string]string {
	env := map[string]string{}

	if !workerPublicIPv4 {
		env["HCLOUD_PUBLIC_IPV4"] = "false"
	}

	if !workerPublicIPv6 {
		env["HCLOUD_PUBLIC_IPV6"] = "false"
	}

	return env
}

// buildChartValues constructs the typed Helm values struct from the given config.
func buildChartValues(
	cfg v1alpha1.NodeAutoscalerConfig,
	haEnabled bool,
	extraEnv map[string]string,
) chartValues {
	scaleDownTime := cfg.ScaleDownUnneededTime
	if scaleDownTime == "" {
		scaleDownTime = defaultScaleDownUnneededTime
	}

	groups := buildAutoscalingGroups(cfg.Pools)

	var replicas int32
	if haEnabled {
		replicas = 2
	}

	return chartValues{
		Replicas:          replicas,
		CloudProvider:     "hetzner",
		AutoscalingGroups: groups,
		ExtraArgs: chartExtraArgs{
			Expander:              expandersToHelmValue(cfg.Expander),
			ScaleDownUnneededTime: scaleDownTime,
			MaxNodesTotal:         cfg.MaxNodesTotal,
			ScaleDownAfterAdd:     "5m",
			ScaleDownAfterDelete:  "2m",
			OkTotalUnreadyCount:   defaultOkTotalUnreadyCount,
			V:                     "4",
		},
		ExtraEnv:        extraEnv,
		ExtraEnvSecrets: buildExtraEnvSecrets(),
		Tolerations: []chartToleration{
			{
				Key:      "node-role.kubernetes.io/control-plane",
				Operator: "Exists",
				Effect:   "NoSchedule",
			},
		},
		NodeSelector: map[string]string{"node-role.kubernetes.io/control-plane": ""},
		RBAC: chartRBAC{
			Create: true,
			ServiceAccount: chartRBACServiceAccount{
				Create: true,
				Name:   "cluster-autoscaler",
			},
			AdditionalRules: coreInformerRBACRules(),
		},
		Resources: chartResources{
			Requests: chartResourceRequests{CPU: "50m", Memory: "128Mi"},
			Limits:   chartResourceLimits{Memory: "256Mi"},
		},
	}
}

// coreInformerRBACRules returns the read-only rules the cluster-autoscaler binary's
// core informers require but the chart's ClusterRole omits. The autoscaler watches
// Deployments (scale-down owner traversal) and ResourceQuotas (quota-aware scale-up
// simulation) unconditionally — i.e. even with the capacity-buffer controller
// disabled — so without these it logs a continuous "deployments.apps is forbidden" /
// "resourcequotas is forbidden" and the informers never sync (ksail#5405). The chart
// only grants apps/{daemonsets,replicasets,statefulsets} and exposes no values knob to
// inject extra rules beyond rbac.additionalRules, so KSail adds them here. They are
// granted in the base values (not gated behind capacity-buffers) because the core
// binary needs them regardless of that feature.
func coreInformerRBACRules() []chartRBACRule {
	readVerbs := []string{"get", "list", "watch"}

	return []chartRBACRule{
		{
			APIGroups: []string{"apps"},
			Resources: []string{"deployments"},
			Verbs:     readVerbs,
		},
		{
			APIGroups: []string{""},
			Resources: []string{"resourcequotas"},
			Verbs:     readVerbs,
		},
	}
}

// buildExtraEnvSecrets returns the secret-backed environment variables the
// cluster-autoscaler reads: the Hetzner token and network (from the "hcloud"
// secret) and the per-pool cluster config (from the autoscaler config secret).
// HCLOUD_CLUSTER_CONFIG carries the snapshot image and per-pool cloud-init,
// labels, and taints, superseding the legacy HCLOUD_IMAGE / HCLOUD_CLOUD_INIT.
func buildExtraEnvSecrets() chartExtraEnvSecrets {
	return chartExtraEnvSecrets{
		HcloudToken:   chartSecretRef{Name: "hcloud", Key: "token"},
		HcloudNetwork: chartSecretRef{Name: "hcloud", Key: "network"},
		HcloudClusterConfig: chartSecretRef{
			Name: AutoscalerConfigSecretName,
			Key:  AutoscalerConfigHcloudClusterConfigKey,
		},
	}
}

// buildAutoscalingGroups converts NodePool specs to autoscalingGroup chart values.
func buildAutoscalingGroups(pools []v1alpha1.NodePool) []autoscalingGroup {
	groups := make([]autoscalingGroup, 0, len(pools))

	for _, pool := range pools {
		groups = append(groups, autoscalingGroup{
			Name:         pool.Name,
			MinSize:      pool.Min,
			MaxSize:      pool.Max,
			InstanceType: pool.ServerType,
			Region:       pool.Location,
		})
	}

	return groups
}

// expandersToHelmValue converts an [v1alpha1.AutoscalerExpanderList] to the
// comma-separated, lowercase kebab-case priority list expected by the
// cluster-autoscaler Helm chart's --expander flag (e.g. "least-nodes,least-waste").
// An empty list falls back to the default expander ("least-waste").
func expandersToHelmValue(expanders v1alpha1.AutoscalerExpanderList) string {
	if len(expanders) == 0 {
		return expanderToHelmValue("")
	}

	parts := make([]string, len(expanders))
	for i, expander := range expanders {
		parts[i] = expanderToHelmValue(expander)
	}

	return strings.Join(parts, ",")
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
