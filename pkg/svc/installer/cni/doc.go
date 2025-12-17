// Package cni provides unified CNI installer implementations and shared utilities
// for managing CNI providers on Kubernetes clusters.
//
// # Overview
//
// The cni package centralizes the logic for installing, configuring, and managing
// Container Network Interface (CNI) providers such as Calico, Cilium, and others.
// It exposes a common interface for CNI installation, enabling consistent workflows
// across different cluster distributions and simplifying the process of adding new CNIs.
//
// Package Structure
//
//	base.go      - Defines InstallerBase and shared utilities for Helm chart installation and readiness checks.
//	base_test.go - Tests for base functionality.
//	doc.go       - This package documentation.
//	calico/      - Implementation of the Calico CNI installer.
//	cilium/      - Implementation of the Cilium CNI installer.
//
// # Adding a New CNI
//
// To add a new CNI installer:
//
//  1. Create a new subdirectory under pkg/svc/installer/cni/ (e.g., pkg/svc/installer/cni/mycni/)
//
//  2. Implement the installer.Installer interface in your new package
//
//  3. Embed InstallerBase in your installer struct to reuse shared Helm and readiness logic:
//
//     type MyCNIInstaller struct {
//     *cni.InstallerBase
//     }
//
//  4. Use the shared helper functions for Helm operations:
//     - helm.InstallOrUpgradeChart() (from pkg/client/helm) for chart installation
//     - installer.WaitForResourceReadiness() (from pkg/svc/installer) for readiness checks
//
//  5. Add comprehensive unit tests following the patterns in existing CNI implementations
//
//  6. Update CONTRIBUTING.md to document the new CNI option
//
// For detailed guidance and code examples, see specs/001-cni-installer-move/quickstart.md.
//
// # Example Usage
//
// Creating and using a CNI installer:
//
//	import (
//	    "context"
//	    "time"
//	    "github.com/devantler-tech/ksail-go/pkg/client/helm"
//	    ciliuminstaller "github.com/devantler-tech/ksail-go/pkg/svc/installer/cni/cilium"
//	)
//
//	helmClient := helm.NewClient(...)
//	installer := ciliuminstaller.NewCiliumInstaller(
//	    helmClient,
//	    "/path/to/kubeconfig",
//	    "my-context",
//	    10*time.Minute,
//	)
//
//	err := installer.Install(context.Background())
//	if err != nil {
//	    log.Fatalf("CNI installation failed: %v", err)
//	}
//
// # Shared Utilities
//
// All CNI installers benefit from shared utilities in InstallerBase:
//
//   - Helm client management for chart operations
//   - Kubeconfig and context handling for cluster access
//   - Timeout management for long-running operations
//   - Standardized readiness checking patterns
//   - Consistent error handling and reporting
//
// These utilities ensure that all CNI installers follow the same patterns and provide
// a consistent experience for users and maintainers.
package cni
