// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

package docs_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEKS_EBScsiSupportClaimsReconciled(t *testing.T) {
	t.Parallel()

	// 1. Verify scaffolded eks.yaml contains aws-ebs-csi-driver
	renderedConfig := string(scaffolder.RenderEKSConfig(
		scaffolder.DefaultEKSConfigParams("test-cluster", "us-east-1"),
	))
	require.Contains(t, renderedConfig, "- name: aws-ebs-csi-driver",
		"eks.yaml scaffold must include aws-ebs-csi-driver addon")

	// 2. Verify support-matrix.mdx documentation
	matrix := readOrSkip(t, "src/content/docs/support-matrix.mdx")

	// The matrix must NOT claim Amazon EBS CSI Driver is "Built-in" for EKS
	assert.NotContains(t, matrix, "| Amazon EBS CSI Driver         | ❌      | ❌       | ❌                  | ❌       | ❌       | Built-in         |",
		"support-matrix must not claim EBS CSI driver is Built-in for EKS")

	// The matrix must label it as Add-on
	assert.Contains(t, matrix, "| Amazon EBS CSI Driver         | ❌      | ❌       | ❌                  | ❌       | ❌       | Add-on¹⁰         |",
		"support-matrix must label EBS CSI driver as Add-on¹⁰ for EKS")

	// Footnote 10 must describe EBS CSI driver as scaffolded add-on, not as absent or separate add-on you must install yourself
	assert.NotContains(t, matrix, "Amazon EBS CSI Driver is not installed by default",
		"footnote 10 must not claim EBS CSI driver is not installed by default")
	assert.NotContains(t, matrix, "the EBS CSI driver is a separate add-on you must install yourself",
		"footnote 10 must not claim EBS CSI driver must be installed manually")
	assert.Contains(t, matrix, "aws-ebs-csi-driver",
		"footnote 10 must mention the scaffolded aws-ebs-csi-driver addon")

	// 3. Verify distributions/eks.mdx documentation
	eksDoc := readOrSkip(t, "src/content/docs/distributions/eks.mdx")
	assert.NotContains(t, eksDoc, "| CSI            | Built-in | Amazon EBS CSI Driver addon",
		"eks.mdx must not claim CSI is Built-in")
	assert.Contains(t, eksDoc, "| CSI            | Add-on   |",
		"eks.mdx must list CSI as Add-on")
	assert.Contains(t, eksDoc, "aws-ebs-csi-driver",
		"eks.mdx must mention aws-ebs-csi-driver")
}
