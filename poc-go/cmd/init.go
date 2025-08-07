package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/devantler-tech/ksail/poc-go/pkg/models"
)

func newInitCommand() *cobra.Command {
	var outputPath string
	var name string
	var containerEngine string
	var distribution string
	var deploymentTool string
	var cni string
	var csi string
	var ingressController string
	var gatewayController string
	var metricsServer bool
	var secretManager string
	var mirrorRegistries bool
	var editor string

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new KSail project",
		Long:  `Initialize a new KSail project with configuration files`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(outputPath, name, containerEngine, distribution, deploymentTool,
				cni, csi, ingressController, gatewayController, metricsServer,
				secretManager, mirrorRegistries, editor)
		},
	}

	// Add flags
	cmd.Flags().StringVarP(&outputPath, "output", "o", "./", "Output directory for the project files")
	cmd.Flags().StringVar(&name, "name", "ksail-default", "Name of the cluster")
	cmd.Flags().StringVar(&containerEngine, "container-engine", "Docker", "Container engine (Docker|Podman)")
	cmd.Flags().StringVar(&distribution, "distribution", "Kind", "Kubernetes distribution (Kind|K3d)")
	cmd.Flags().StringVar(&deploymentTool, "deployment-tool", "Kubectl", "Deployment tool (Kubectl|Flux)")
	cmd.Flags().StringVar(&cni, "cni", "Default", "Container Network Interface (Default|Cilium|None)")
	cmd.Flags().StringVar(&csi, "csi", "Default", "Container Storage Interface (Default|LocalPathProvisioner|None)")
	cmd.Flags().StringVar(&ingressController, "ingress-controller", "Default", "Ingress Controller (Default|Traefik|None)")
	cmd.Flags().StringVar(&gatewayController, "gateway-controller", "Default", "Gateway Controller (Default|None)")
	cmd.Flags().BoolVar(&metricsServer, "metrics-server", true, "Whether to install Metrics Server")
	cmd.Flags().StringVar(&secretManager, "secret-manager", "None", "Secret manager (None|SOPS)")
	cmd.Flags().BoolVar(&mirrorRegistries, "mirror-registries", true, "Whether to set up mirror registries")
	cmd.Flags().StringVar(&editor, "editor", "Nano", "Editor to use (Nano|Vim)")

	return cmd
}

func runInit(outputPath, name, containerEngine, distribution, deploymentTool,
	cni, csi, ingressController, gatewayController string, metricsServer bool,
	secretManager string, mirrorRegistries bool, editor string) error {

	fmt.Printf("Initializing KSail project in %s...\n", outputPath)

	// Create the cluster configuration
	cluster := models.NewDefaultCluster(name)
	
	// Apply user-provided options
	cluster.Spec.ContainerEngine = models.KSailContainerEngineType(containerEngine)
	cluster.Spec.Distribution = models.KSailDistributionType(distribution)
	cluster.Spec.DeploymentTool = models.KSailDeploymentToolType(deploymentTool)
	cluster.Spec.CNI = models.KSailCNIType(cni)
	cluster.Spec.CSI = models.KSailCSIType(csi)
	cluster.Spec.IngressController = models.KSailIngressControllerType(ingressController)
	cluster.Spec.GatewayController = models.KSailGatewayControllerType(gatewayController)
	cluster.Spec.MetricsServer = metricsServer
	cluster.Spec.SecretManager = models.KSailSecretManagerType(secretManager)
	cluster.Spec.MirrorRegistries = mirrorRegistries
	cluster.Spec.Editor = models.KSailEditorType(editor)

	// Ensure output directory exists
	if err := os.MkdirAll(outputPath, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Generate ksail.yaml
	if err := generateKSailConfig(cluster, outputPath); err != nil {
		return fmt.Errorf("failed to generate ksail.yaml: %w", err)
	}

	// Generate distribution config (kind.yaml or k3d.yaml)
	if err := generateDistributionConfig(cluster, outputPath); err != nil {
		return fmt.Errorf("failed to generate distribution config: %w", err)
	}

	// Generate k8s directory and kustomization.yaml
	if err := generateKustomization(cluster, outputPath); err != nil {
		return fmt.Errorf("failed to generate kustomization: %w", err)
	}

	// Generate SOPS config if needed
	if cluster.Spec.SecretManager == models.SecretManagerSOPS {
		if err := generateSOPSConfig(cluster, outputPath); err != nil {
			return fmt.Errorf("failed to generate SOPS config: %w", err)
		}
	}

	fmt.Println("✓ KSail project initialized successfully!")
	fmt.Printf("✓ Generated ksail.yaml\n")
	fmt.Printf("✓ Generated %s.yaml\n", string(cluster.Spec.Distribution))
	fmt.Printf("✓ Generated %s/kustomization.yaml\n", cluster.Spec.KustomizationPath)
	if cluster.Spec.SecretManager == models.SecretManagerSOPS {
		fmt.Printf("✓ Generated .sops.yaml\n")
	}
	
	return nil
}

func generateKSailConfig(cluster *models.KSailCluster, outputPath string) error {
	configPath := filepath.Join(outputPath, cluster.Spec.ConfigPath)
	
	file, err := os.Create(configPath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := yaml.NewEncoder(file)
	encoder.SetIndent(2)
	
	return encoder.Encode(cluster)
}

func generateDistributionConfig(cluster *models.KSailCluster, outputPath string) error {
	var configContent string
	var fileName string

	switch cluster.Spec.Distribution {
	case models.DistributionKind:
		fileName = "kind.yaml"
		configContent = generateKindConfig(cluster)
	case models.DistributionK3d:
		fileName = "k3d.yaml"
		configContent = generateK3dConfig(cluster)
	default:
		return fmt.Errorf("unsupported distribution: %s", cluster.Spec.Distribution)
	}

	configPath := filepath.Join(outputPath, fileName)
	return os.WriteFile(configPath, []byte(configContent), 0644)
}

func generateKindConfig(cluster *models.KSailCluster) string {
	return fmt.Sprintf(`# Kind cluster configuration for %s
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: %s
nodes:
- role: control-plane
  kubeadmConfigPatches:
  - |
    kind: InitConfiguration
    nodeRegistration:
      kubeletExtraArgs:
        node-labels: "ingress-ready=true"
  extraPortMappings:
  - containerPort: 80
    hostPort: 80
    protocol: TCP
  - containerPort: 443
    hostPort: 443
    protocol: TCP
`, cluster.Metadata.Name, cluster.Metadata.Name)
}

func generateK3dConfig(cluster *models.KSailCluster) string {
	return fmt.Sprintf(`# K3d cluster configuration for %s
apiVersion: k3d.io/v1alpha5
kind: Simple
metadata:
  name: %s
servers: 1
agents: 0
ports:
  - port: 80:80
    nodeFilters:
      - loadbalancer
  - port: 443:443
    nodeFilters:
      - loadbalancer
`, cluster.Metadata.Name, cluster.Metadata.Name)
}

func generateKustomization(cluster *models.KSailCluster, outputPath string) error {
	k8sPath := filepath.Join(outputPath, cluster.Spec.KustomizationPath)
	if err := os.MkdirAll(k8sPath, 0755); err != nil {
		return err
	}

	kustomizationContent := `apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization

# Add your Kubernetes manifests here
resources: []

# Add any patches here
patches: []

# Add any images here
images: []
`

	kustomizationPath := filepath.Join(k8sPath, "kustomization.yaml")
	return os.WriteFile(kustomizationPath, []byte(kustomizationContent), 0644)
}

func generateSOPSConfig(cluster *models.KSailCluster, outputPath string) error {
	sopsContent := `creation_rules:
  - path_regex: \.yaml$
    age: >-
      # Add your public key here
`

	sopsPath := filepath.Join(outputPath, ".sops.yaml")
	return os.WriteFile(sopsPath, []byte(sopsContent), 0644)
}