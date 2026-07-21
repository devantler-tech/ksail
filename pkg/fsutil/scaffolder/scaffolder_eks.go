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

	clusterName := defaultEKSClusterName
	if s.ClusterName != "" {
		clusterName = s.ClusterName
	}

	content := RenderEKSConfig(DefaultEKSConfigParams(clusterName, defaultEKSRegion))

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

// EKSConfigParams are the values rendered into an eksctl ClusterConfig. It is the single shape used
// both by `ksail init` scaffolding and by runtime EKS provisioning (e.g. the web UI), so the
// generated config never drifts between the two.
type EKSConfigParams struct {
	ClusterName       string
	Region            string
	KubernetesVersion string
	NodeGroupName     string
	InstanceType      string
	AMIFamily         string
	DesiredCapacity   int32
	MinSize           int32
	MaxSize           int32
}

// DefaultEKSConfigParams returns the default eksctl parameters with the given cluster name and
// region applied. An empty region falls back to the scaffolding default.
func DefaultEKSConfigParams(clusterName, region string) EKSConfigParams {
	if region == "" {
		region = defaultEKSRegion
	}

	return EKSConfigParams{
		ClusterName:       clusterName,
		Region:            region,
		KubernetesVersion: defaultEKSVersion,
		NodeGroupName:     defaultEKSNodeGroupName,
		InstanceType:      defaultEKSInstanceType,
		AMIFamily:         defaultEKSAMIFamily,
		DesiredCapacity:   defaultEKSDesiredCapacity,
		MinSize:           defaultEKSMinSize,
		MaxSize:           defaultEKSMaxSize,
	}
}

// RenderEKSConfig renders an eksctl ClusterConfig YAML document from params.
func RenderEKSConfig(params EKSConfigParams) []byte {
	return fmt.Appendf(nil, eksDefaultConfigTemplate,
		params.ClusterName,
		params.Region,
		params.KubernetesVersion,
		params.NodeGroupName,
		params.InstanceType,
		params.DesiredCapacity,
		params.MinSize,
		params.MaxSize,
		params.AMIFamily,
	)
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
