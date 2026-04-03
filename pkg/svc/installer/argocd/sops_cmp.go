package argocdinstaller

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
)

// ShouldEnableSOPS returns true if SOPS CMP support should be enabled for ArgoCD.
// The decision mirrors EnsureSopsAgeSecret: explicitly disabled → false,
// explicitly enabled → true, auto-detect → true only if an Age key is available.
func ShouldEnableSOPS(sops v1alpha1.SOPS) bool {
	if sops.Enabled != nil && !*sops.Enabled {
		return false
	}

	if sops.Enabled != nil && *sops.Enabled {
		return true
	}

	// Auto-detect: check if key is available
	key, err := resolveAgeKey(sops)

	return err == nil && key != ""
}

// buildSOPSValuesYaml returns the Helm values YAML fragment that configures
// a CMP sidecar for SOPS Age decryption on the ArgoCD repo-server.
func buildSOPSValuesYaml() string {
	sopsVer := sopsVersion()
	image := appImage()

	return fmt.Sprintf(`configs:
  cmp:
    create: true
    plugins:
      kustomize-sops:
        discover:
          find:
            command:
              - sh
              - -c
              - "find . -type f \\( -name '*.yaml' -o -name '*.yml' \\) -exec grep -l '^sops:' {} \\; | head -1 | grep -q ."
        generate:
          command:
            - sh
            - -c
          args:
            - |
              find . -type f \( -name '*.yaml' -o -name '*.yml' \) -exec grep -l '^sops:' {} \; | while read -r f; do
                sops --decrypt --in-place "$f"
              done
              if [ -f kustomization.yaml ] || [ -f kustomization.yml ] || [ -f Kustomization ]; then
                kustomize build .
              else
                find . -type f \( -name '*.yaml' -o -name '*.yml' \) | sort | while read -r f; do
                  echo '---'
                  cat "$f"
                done
              fi
repoServer:
  initContainers:
    - name: install-sops
      image: alpine:3.21
      command:
        - sh
        - -c
      args:
        - |
          ARCH=$(uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
          wget -qO /custom-tools/sops "https://github.com/getsops/sops/releases/download/v%[1]s/sops-v%[1]s.linux.${ARCH}"
          chmod +x /custom-tools/sops
      volumeMounts:
        - name: custom-tools
          mountPath: /custom-tools
  extraContainers:
    - name: cmp-kustomize-sops
      command:
        - /var/run/argocd/argocd-cmp-server
      image: %[2]s
      env:
        - name: SOPS_AGE_KEY_FILE
          value: /sops/age/sops.agekey
      securityContext:
        runAsNonRoot: true
        runAsUser: 999
      volumeMounts:
        - mountPath: /var/run/argocd
          name: var-files
        - mountPath: /home/argocd/cmp-server/plugins
          name: plugins
        - mountPath: /home/argocd/cmp-server/config/plugin.yaml
          subPath: kustomize-sops.yaml
          name: argocd-cmp-cm
        - mountPath: /tmp
          name: cmp-tmp
        - mountPath: /usr/local/bin/sops
          name: custom-tools
          subPath: sops
        - mountPath: /sops/age
          name: sops-age
          readOnly: true
  volumes:
    - name: argocd-cmp-cm
      configMap:
        name: argocd-cmp-cm
    - name: cmp-tmp
      emptyDir: {}
    - name: custom-tools
      emptyDir: {}
    - name: sops-age
      secret:
        secretName: sops-age
        optional: true
`, sopsVer, image)
}
