package scaffolder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EKSConfigFile is the default eksctl configuration filename.
const EKSConfigFile = "eksctl.yaml"

// generateEKSConfig generates the eksctl.yaml configuration file.
// It scaffolds a minimal declarative eksctl `ClusterConfig` with a single
// managed node group, IAM OIDC enabled, and the addons KSail considers
// "provided by default" for EKS (VPC CNI, kube-proxy, CoreDNS, EBS CSI).
// Users are expected to edit the file to match their AWS environment.
func (s *Scaffolder) generateEKSConfig(output string, force bool) error {
	configPath := filepath.Join(output, EKSConfigFile)

	skip, existed, previousModTime := s.checkFileExistsAndSkip(
		configPath,
		EKSConfigFile,
		force,
	)
	if skip {
		return nil
	}

	clusterName := "eks-default"

	region := strings.TrimSpace(s.KSailConfig.Spec.Cluster.EKS.Region)
	if region == "" {
		region = "us-east-1"
	}

	version := strings.TrimSpace(s.KSailConfig.Spec.Cluster.EKS.KubernetesVersion)
	if version == "" {
		version = "1.31"
	}

	nodeGroupName := strings.TrimSpace(s.KSailConfig.Spec.Cluster.EKS.NodeGroupName)
	if nodeGroupName == "" {
		nodeGroupName = "default"
	}

	instanceType := strings.TrimSpace(s.KSailConfig.Spec.Cluster.EKS.InstanceType)
	if instanceType == "" {
		instanceType = "t3.medium"
	}

	desiredCapacity := s.KSailConfig.Spec.Cluster.EKS.DesiredCapacity
	if desiredCapacity <= 0 {
		desiredCapacity = 2
	}

	minSize := s.KSailConfig.Spec.Cluster.EKS.MinSize
	if minSize <= 0 {
		minSize = 1
	}

	maxSize := s.KSailConfig.Spec.Cluster.EKS.MaxSize
	if maxSize <= 0 {
		maxSize = 3
	}

	amiFamily := strings.TrimSpace(s.KSailConfig.Spec.Cluster.EKS.AMIFamily)
	if amiFamily == "" {
		amiFamily = "AmazonLinux2023"
	}

	content := []byte(fmt.Sprintf(eksDefaultConfigTemplate,
		clusterName,
		region,
		version,
		nodeGroupName,
		instanceType,
		desiredCapacity,
		minSize,
		maxSize,
		amiFamily,
	))

	err := os.WriteFile(configPath, content, filePerm)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrEKSConfigGeneration, err)
	}

	if force && existed {
		err = ensureOverwriteModTime(configPath, previousModTime)
		if err != nil {
			return fmt.Errorf("failed to update mod time for %s: %w", EKSConfigFile, err)
		}
	}

	s.notifyFileAction(EKSConfigFile, existed)

	return nil
}

// eksDefaultConfigTemplate is the scaffolded eksctl ClusterConfig.
// See https://eksctl.io/usage/schema/ for the full schema.
// The placeholders are, in order: cluster name, region, Kubernetes version,
// nodegroup name, instance type, desiredCapacity, minSize, maxSize.
const eksDefaultConfigTemplate = `# eksctl cluster configuration.
# See https://eksctl.io/usage/schema/ for the full schema.
apiVersion: eksctl.io/v1alpha5
kind: ClusterConfig

metadata:
  name: %s
  region: %s
  version: "%s"

iam:
  withOIDC: true

addons:
  - name: vpc-cni
  - name: kube-proxy
  - name: coredns
  - name: aws-ebs-csi-driver

managedNodeGroups:
  - name: %s
    instanceType: %s
    desiredCapacity: %d
    minSize: %d
    maxSize: %d
    volumeSize: 20
    amiFamily: %s
`
