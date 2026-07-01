package clusterautoscalerinstaller

import (
	_ "embed"
	"fmt"

	"sigs.k8s.io/yaml"
)

// capacityBufferCRDYAML is the CapacityBuffer CRD
// (capacitybuffers.autoscaling.x-k8s.io) vendored verbatim from the
// kubernetes/autoscaler repository. The cluster-autoscaler Helm chart does not
// ship this CRD, so KSail delivers it through the chart's extraObjects value
// when spec.cluster.autoscaler.node.capacityBuffers is enabled.
//
// VERSION TRACKING: the CRD must match the Cluster Autoscaler app version
// deployed by the pinned chart (see Chart.yaml: chart 9.57.0 = appVersion
// 1.35.0, where v1alpha1 is deprecated and v1beta1 is the storage version).
// When a chart bump changes the appVersion, re-sync this file from the
// matching release branch:
//
//	https://raw.githubusercontent.com/kubernetes/autoscaler/cluster-autoscaler-release-1.35/
//	cluster-autoscaler/apis/config/crd/autoscaling.x-k8s.io_capacitybuffers.yaml
//
// (replace "1.35" with the new appVersion's minor release).
//
//go:embed autoscaling.x-k8s.io_capacitybuffers.yaml
var capacityBufferCRDYAML []byte

// enableCapacityBuffers mutates vals to turn on the upstream capacity-buffers
// feature (Cluster Autoscaler 1.34+, off by default). It wires up the three
// pieces the feature needs:
//
//  1. Flags: capacity-buffer-controller-enabled and
//     capacity-buffer-pod-injection-enabled, so the autoscaler reconciles
//     CapacityBuffer resources and injects virtual pods for ready buffers; plus
//     kube-api-content-type=application/json, because the buffer controller's
//     client defaults to protobuf (the autoscaler's built-in-type encoding) which
//     the CapacityBuffer CRD cannot satisfy — without JSON the controller errors
//     and never writes buffer status, silently disabling over-provisioning
//     (ksail#5603).
//  2. RBAC: the chart's ClusterRole only grants provisioningrequests in the
//     autoscaling.x-k8s.io group, so the buffer controller's access to the
//     CapacityBuffer CRs themselves is added via the chart's rbac.additionalRules
//     value. The Deployments and ResourceQuotas its translators run informers over
//     (to size buffers from a workload's template and respect namespace quota) are
//     already granted unconditionally by coreInformerRBACRules in the base values —
//     the core autoscaler needs them too (ksail#5405) — so only the CapacityBuffer
//     rule is added here.
//  3. CRD: the chart does not ship the CapacityBuffer CRD; it is delivered via
//     the chart's extraObjects value. NOTE: because the CRD is then part of the
//     Helm release, `helm uninstall` removes the CRD — and with it every
//     CapacityBuffer resource in the cluster.
func enableCapacityBuffers(vals *chartValues) error {
	vals.ExtraArgs.CapacityBufferControllerEnabled = true
	vals.ExtraArgs.CapacityBufferPodInjectionEnabled = true
	// The CapacityBuffer controller reconciles a CRD, but the autoscaler's client
	// defaults to protobuf (built-in-type encoding) which CRDs cannot satisfy — force
	// JSON for the whole autoscaler client. Negligible overhead on the small clusters
	// that use capacity buffers; without it buffer status never populates and
	// over-provisioning is silently a no-op (ksail#5603).
	vals.ExtraArgs.KubeAPIContentType = "application/json"

	// The CapacityBuffer CRs themselves; the Deployments and ResourceQuotas the
	// buffer controller's translators run informers over are already covered by
	// coreInformerRBACRules in the base values (the core autoscaler needs them too).
	vals.RBAC.AdditionalRules = append(vals.RBAC.AdditionalRules,
		chartRBACRule{
			APIGroups: []string{"autoscaling.x-k8s.io"},
			Resources: []string{"capacitybuffers", "capacitybuffers/status"},
			Verbs:     []string{"get", "list", "watch", "update", "patch"},
		},
	)

	// The embedded CRD is a single YAML document; unmarshal it into a generic
	// object for the chart's extraObjects values list (sigs.k8s.io/yaml routes
	// through JSON, so the result is a plain map suitable for re-marshaling).
	var crd map[string]any

	err := yaml.Unmarshal(capacityBufferCRDYAML, &crd)
	if err != nil {
		return fmt.Errorf("parse embedded CapacityBuffer CRD: %w", err)
	}

	vals.ExtraObjects = append(vals.ExtraObjects, crd)

	return nil
}
