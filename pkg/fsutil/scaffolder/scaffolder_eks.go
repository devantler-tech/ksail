package scaffolder

import (
	"fmt"
	"os"
	"path/filepath"
)

// EKSConfigFile is the default eksctl configuration filename.
const EKSConfigFile = "eks.yaml"

const (
	defaultEKSClusterName           = "eks-default"
	defaultEKSRegion                = "us-east-1"
	defaultEKSVersion               = "1.31"
	defaultEKSNodeGroupName         = "default"
	defaultEKSInstanceType          = "t3.medium"
	defaultEKSAMIFamily             = "AmazonLinux2023"
	defaultEKSDesiredCapacity int32 = 2
	defaultEKSMinSize         int32 = 1
	defaultEKSMaxSize         int32 = 3
)

// generateEKSConfig generates the eks.yaml (eksctl ClusterConfig) file.
// It scaffolds a minimal declarative eksctl `ClusterConfig` with a single
// managed node group, IAM OIDC enabled, and the addons KSail considers
// "provided by default" for EKS (VPC CNI, kube-proxy, CoreDNS, EBS CSI).
// Users are expected to edit the file to match their AWS environment.
// All cluster metadata lives in this file — it is the authoritative source of
// truth for the EKS cluster definition.
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

	content := fmt.Appendf(nil, eksDefaultConfigTemplate,
		defaultEKSClusterName,
		defaultEKSRegion,
		defaultEKSVersion,
		defaultEKSNodeGroupName,
		defaultEKSInstanceType,
		defaultEKSDesiredCapacity,
		defaultEKSMinSize,
		defaultEKSMaxSize,
		defaultEKSAMIFamily,
	)

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
// nodegroup name, instance type, desiredCapacity, minSize, maxSize, amiFamily.
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
