package scaffolder

import (
	"fmt"
	"os"
	"path/filepath"
)

// KWOKConfigDir is the default KWOK configuration directory name.
// KWOK's config loader auto-detects directories and runs kustomize to assemble configs.
const KWOKConfigDir = "kwok"

// KWOKConfigFile is an alias for KWOKConfigDir for backward compatibility.
// Deprecated: Use KWOKConfigDir instead.
const KWOKConfigFile = KWOKConfigDir

// KWOKSimulationFile is the filename for the simulation CRDs within the kwok directory.
const KWOKSimulationFile = "simulation.yaml"

// KWOKNodeNotReadyFile is the filename for the node-not-ready chaos stage.
const KWOKNodeNotReadyFile = "node-not-ready.yaml"

// KWOKPodFailureFile is the filename for the pod container failure chaos stage.
const KWOKPodFailureFile = "pod-failure.yaml"

// generateKWOKConfig generates the kwok/ kustomize directory.
// It scaffolds a kustomization.yaml that references simulation.yaml (the 4 Cluster-level
// CRDs not provided by KWOK by default) and includes commented-out references to optional
// CEL-based chaos stages (node-not-ready, pod-failure).
func (s *Scaffolder) generateKWOKConfig(output string, force bool) error {
	kwokDir := filepath.Join(output, KWOKConfigDir)

	skip, existed, previousModTime := s.checkFileExistsAndSkip(
		kwokDir,
		KWOKConfigDir,
		force,
	)
	if skip {
		return nil
	}

	err := os.MkdirAll(kwokDir, dirPerm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKWOKConfigGeneration, err)
	}

	// Write kustomization.yaml
	kustomizationPath := filepath.Join(kwokDir, "kustomization.yaml")

	err = os.WriteFile(kustomizationPath, []byte(KWOKKustomizationConfig), filePerm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKWOKConfigGeneration, err)
	}

	// Write simulation.yaml (the 4 Cluster-level CRDs)
	simulationPath := filepath.Join(kwokDir, KWOKSimulationFile)

	err = os.WriteFile(simulationPath, []byte(KWOKDefaultSimulationConfig), filePerm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKWOKConfigGeneration, err)
	}

	// Write CEL chaos stage examples
	nodeNotReadyPath := filepath.Join(kwokDir, KWOKNodeNotReadyFile)

	err = os.WriteFile(nodeNotReadyPath, []byte(KWOKNodeNotReadyStage), filePerm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKWOKConfigGeneration, err)
	}

	podFailurePath := filepath.Join(kwokDir, KWOKPodFailureFile)

	err = os.WriteFile(podFailurePath, []byte(KWOKPodFailureStage), filePerm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKWOKConfigGeneration, err)
	}

	if force && existed {
		err = ensureOverwriteModTime(kwokDir, previousModTime)
		if err != nil {
			return fmt.Errorf("failed to update mod time for %s: %w", KWOKConfigDir, err)
		}
	}

	s.notifyFileAction(KWOKConfigDir, existed)

	return nil
}

// KWOKKustomizationConfig is the kustomization.yaml for the kwok/ directory.
// It references simulation.yaml by default and includes commented-out references
// to CEL chaos stages that users can opt-in to.
const KWOKKustomizationConfig = `# KWOK kustomize configuration.
# KWOK v0.7.0+ natively loads kustomize directories via --config.
# Uncomment chaos stages below to enable simulation scenarios.
# See https://kwok.sigs.k8s.io/docs/user/ for all available options.
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - simulation.yaml
  # Uncomment to enable chaos simulation stages (KWOK v0.7.0+ CEL support):
  # - node-not-ready.yaml
  # - pod-failure.yaml
`

// KWOKDefaultSimulationConfig contains the default KWOK configuration YAML.
// It configures the four Cluster-level CRDs that KWOK does NOT provide by default,
// enabling kubectl logs, exec, attach, and port-forward to work out of the box.
// Used by both the scaffolder (ksail cluster init) and the provisioner (in-memory
// fallback when no kwok.yaml is provided).
const KWOKDefaultSimulationConfig = `# KWOK cluster simulation configuration.
# These CRDs enable kubectl logs, exec, attach, and port-forward for simulated pods.
# See https://kwok.sigs.k8s.io/docs/user/ for available options.
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterLogs
metadata:
  name: default-logs
spec:
  logs:
    - logsFile: /dev/null
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterExec
metadata:
  name: default-exec
spec:
  execs:
    - local: {}
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterAttach
metadata:
  name: default-attach
spec:
  attaches:
    - {}
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterPortForward
metadata:
  name: default-port-forward
spec:
  forwards:
    - {}
`

// KWOKNodeNotReadyStage defines a Stage that transitions nodes to NotReady when labeled.
// Uses KWOK v0.7.0 CEL expressions for selector matching.
// Apply the label to trigger: kubectl label node <name> node-not-ready.stage.kwok.x-k8s.io=true
const KWOKNodeNotReadyStage = `# Node NotReady chaos stage (KWOK v0.7.0+ CEL support).
# Transitions labeled nodes to NotReady status after a configurable delay.
# Usage: kubectl label node <name> node-not-ready.stage.kwok.x-k8s.io=true
# See https://kwok.sigs.k8s.io/docs/user/stages-configuration/
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: Stage
metadata:
  name: node-not-ready
spec:
  resourceRef:
    apiGroup: v1
    kind: Node
  selector:
    matchExpressions:
      - cel:
          expression: >-
            has(self.metadata.labels) &&
            'node-not-ready.stage.kwok.x-k8s.io' in self.metadata.labels &&
            self.metadata.labels['node-not-ready.stage.kwok.x-k8s.io'] == 'true'
      - cel:
          expression: self.status.phase == 'Running'
  delay:
    durationMilliseconds: 1000
    jitterDurationMilliseconds: 2000
  weight: 10000
  steps:
    - patch:
        subresource: status
        type: merge
        template: |-
          {{ $now := Now }}
          conditions:
          {{ range NodeConditions }}
          {{ if eq .type "Ready" }}
          - lastHeartbeatTime: {{ $now | Quote }}
            lastTransitionTime: {{ $now | Quote }}
            message: "node simulated as NotReady by KWOK stage"
            reason: "KWOKChaosStage"
            status: "False"
            type: {{ .type | Quote }}
          {{ else }}
          - lastHeartbeatTime: {{ $now | Quote }}
            lastTransitionTime: {{ $now | Quote }}
            message: {{ .message | Quote }}
            reason: {{ .reason | Quote }}
            status: {{ .status | Quote }}
            type: {{ .type | Quote }}
          {{ end }}
          {{ end }}
    - event:
        type: Warning
        reason: KWOKChaosStage
        message: "Node transitioned to NotReady by KWOK chaos stage"
`

// KWOKPodFailureStage defines a Stage that fails running pod containers when labeled.
// Uses KWOK v0.7.0 CEL expressions for selector matching.
// Apply the label to trigger: kubectl label pod <name> pod-failure.stage.kwok.x-k8s.io=true
const KWOKPodFailureStage = `# Pod container failure chaos stage (KWOK v0.7.0+ CEL support).
# Transitions labeled pods to Failed status after a configurable delay.
# Usage: kubectl label pod <name> pod-failure.stage.kwok.x-k8s.io=true
# See https://kwok.sigs.k8s.io/docs/user/stages-configuration/
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: Stage
metadata:
  name: pod-container-failure
spec:
  resourceRef:
    apiGroup: v1
    kind: Pod
  selector:
    matchExpressions:
      - cel:
          expression: >-
            has(self.metadata.labels) &&
            'pod-failure.stage.kwok.x-k8s.io' in self.metadata.labels &&
            self.metadata.labels['pod-failure.stage.kwok.x-k8s.io'] == 'true'
      - cel:
          expression: >-
            !has(self.metadata.deletionTimestamp)
      - cel:
          expression: self.status.phase == 'Running'
  delay:
    durationMilliseconds: 1000
    jitterDurationMilliseconds: 2000
  weight: 10000
  steps:
    - patch:
        subresource: status
        type: merge
        template: |-
          {{ $now := Now }}
          phase: Failed
          conditions:
          - lastProbeTime: null
            lastTransitionTime: {{ $now | Quote }}
            status: "True"
            type: Initialized
          - lastTransitionTime: {{ $now | Quote }}
            status: "False"
            reason: "ContainersFailed"
            type: Ready
          - lastTransitionTime: {{ $now | Quote }}
            status: "False"
            reason: "ContainersFailed"
            type: ContainersReady
          containerStatuses:
          {{ range .spec.containers }}
          - image: {{ .image | Quote }}
            name: {{ .name | Quote }}
            ready: false
            restartCount: 0
            started: false
            state:
              terminated:
                exitCode: 1
                finishedAt: {{ $now | Quote }}
                reason: "KWOKChaosStage"
                startedAt: {{ $now | Quote }}
          {{ end }}
    - event:
        type: Warning
        reason: KWOKChaosStage
        message: "Pod containers failed by KWOK chaos stage"
`
