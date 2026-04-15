package scaffolder

import (
	"fmt"
	"os"
	"path/filepath"
)

// KWOKConfigFile is the default KWOK configuration filename.
const KWOKConfigFile = "kwok.yaml"

// generateKWOKConfig generates the kwok.yaml configuration file.
// It scaffolds CRDs NOT provided by KWOK by default: ClusterLogs, ClusterExec,
// ClusterAttach, and ClusterPortForward. KWOK already provides Node and Pod
// Stage configurations (node-init, node-heartbeat, pod-ready, pod-complete,
// pod-delete) by default.
func (s *Scaffolder) generateKWOKConfig(output string, force bool) error {
	configPath := filepath.Join(output, KWOKConfigFile)

	skip, existed, previousModTime := s.checkFileExistsAndSkip(
		configPath,
		KWOKConfigFile,
		force,
	)
	if skip {
		return nil
	}

	content := []byte(KWOKDefaultSimulationConfig)

	err := os.WriteFile(configPath, content, filePerm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrKWOKConfigGeneration, err)
	}

	if force && existed {
		err = ensureOverwriteModTime(configPath, previousModTime)
		if err != nil {
			return fmt.Errorf("failed to update mod time for %s: %w", KWOKConfigFile, err)
		}
	}

	s.notifyFileAction(KWOKConfigFile, existed)

	return nil
}

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
  selector: {}
  logs:
    - containers:
        - name: '*'
      logsFile: /dev/null
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterExec
metadata:
  name: default-exec
spec:
  selector: {}
  execs:
    - containers:
        - name: '*'
      command:
        - /bin/sh
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterAttach
metadata:
  name: default-attach
spec:
  selector: {}
  attaches:
    - containers:
        - name: '*'
---
apiVersion: kwok.x-k8s.io/v1alpha1
kind: ClusterPortForward
metadata:
  name: default-port-forward
spec:
  selector: {}
  forwards:
    - ports:
        - name: '*'
`
