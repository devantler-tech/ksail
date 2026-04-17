package scaffolder

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v6/pkg/apis/cluster/v1alpha1"
)

// EKSConfigFile is the default eksctl configuration filename.
const EKSConfigFile = "eksctl.yaml"

// eksConfigDefaults holds resolved defaults for the scaffolded eksctl.yaml.
// Zero and empty values from the user's KSail configuration are replaced with
// sensible starting points so the scaffolded file is ready to apply.
type eksConfigDefaults struct {
	clusterName     string
	region          string
	version         string
	nodeGroupName   string
	instanceType    string
	amiFamily       string
	desiredCapacity int32
	minSize         int32
	maxSize         int32
}

func firstNonEmpty(value, fallback string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}

	return fallback
}

func firstPositiveInt32(value, fallback int32) int32 {
	if value > 0 {
		return value
	}

	return fallback
}

const (
	defaultEKSDesiredCapacity int32 = 2
	defaultEKSMinSize         int32 = 1
	defaultEKSMaxSize         int32 = 3
)

func resolveEKSDefaults(opts v1alpha1.OptionsEKS) eksConfigDefaults {
	return eksConfigDefaults{
		clusterName:     "eks-default",
		region:          firstNonEmpty(opts.Region, "us-east-1"),
		version:         firstNonEmpty(opts.KubernetesVersion, "1.31"),
		nodeGroupName:   firstNonEmpty(opts.NodeGroupName, "default"),
		instanceType:    firstNonEmpty(opts.InstanceType, "t3.medium"),
		amiFamily:       firstNonEmpty(opts.AMIFamily, "AmazonLinux2023"),
		desiredCapacity: firstPositiveInt32(opts.DesiredCapacity, defaultEKSDesiredCapacity),
		minSize:         firstPositiveInt32(opts.MinSize, defaultEKSMinSize),
		maxSize:         firstPositiveInt32(opts.MaxSize, defaultEKSMaxSize),
	}
}

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

	defaults := resolveEKSDefaults(s.KSailConfig.Spec.Cluster.EKS)

	content := fmt.Appendf(nil, eksDefaultConfigTemplate,
		defaults.clusterName,
		defaults.region,
		defaults.version,
		defaults.nodeGroupName,
		defaults.instanceType,
		defaults.desiredCapacity,
		defaults.minSize,
		defaults.maxSize,
		defaults.amiFamily,
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
