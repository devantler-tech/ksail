package argocdinstaller

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/sopsutil"
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
	key, err := sopsutil.ResolveAgeKey(sops)

	return err == nil && key != ""
}

// buildSOPSValuesYaml returns the Helm values YAML fragment that configures
// a CMP sidecar for SOPS Age decryption on the ArgoCD repo-server.
func buildSOPSValuesYaml() string {
	var b strings.Builder

	b.WriteString(buildCMPPluginYaml())
	b.WriteString(buildRepoServerYaml())

	return b.String()
}

// buildCMPPluginYaml returns the configs.cmp section that defines the
// kustomize-sops Config Management Plugin.
func buildCMPPluginYaml() string {
	return `configs:
  cmp:
    create: true
    plugins:
      kustomize-sops:
        discover:
          find:
            command:
              - sh
              - -c
              - >-
                find . -type f \( -name '*.yaml' -o -name '*.yml' \)
                -exec grep -l '^sops:' {} \; | head -1 | grep -q .
        generate:
          command:
            - sh
            - -c
          args:
            - |
              find . -type f \( -name '*.yaml' -o -name '*.yml' \) \
                -exec grep -l '^sops:' {} \; | while read -r f; do
                sops --decrypt --in-place "$f"
              done
              if [ -f kustomization.yaml ] || [ -f kustomization.yml ] \
                || [ -f Kustomization ]; then
                kustomize build .
              else
                find . -type f \( -name '*.yaml' -o -name '*.yml' \) \
                  | sort | while read -r f; do
                  echo '---'
                  cat "$f"
                done
              fi
`
}

// buildRepoServerYaml returns the repoServer section that configures
// the init container, CMP sidecar, and volumes for SOPS decryption.
func buildRepoServerYaml() string {
	var b strings.Builder

	b.WriteString(buildInitContainerYaml())
	b.WriteString(buildSidecarAndVolumesYaml())

	return b.String()
}

// buildInitContainerYaml returns the SOPS binary init container section.
// Uses the official getsops/sops image to copy the binary, avoiding
// runtime network downloads and supply-chain risks.
func buildInitContainerYaml() string {
	sopsVer := sopsVersion()

	return fmt.Sprintf(`repoServer:
  initContainers:
    - name: install-sops
      image: ghcr.io/getsops/sops:v%s-alpine
      command:
        - sh
        - -c
      args:
        - cp /usr/local/bin/sops /custom-tools/sops
      volumeMounts:
        - name: custom-tools
          mountPath: /custom-tools
`, sopsVer)
}

// buildSidecarAndVolumesYaml returns the CMP sidecar container and
// required volume definitions.
func buildSidecarAndVolumesYaml() string {
	image := appImage()

	return fmt.Sprintf(`  extraContainers:
    - name: cmp-kustomize-sops
      command:
        - /var/run/argocd/argocd-cmp-server
      image: %s
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
`, image)
}
