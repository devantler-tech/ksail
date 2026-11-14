// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

package docs

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// ErrUnlabelledDistribution is returned when a distribution has no row label in the generated
// support matrix, so a new distribution cannot be left out of it silently.
var ErrUnlabelledDistribution = errors.New("distribution has no support-matrix row label")

// ErrProviderColumnsMismatch is returned when the support matrix's provider columns are not
// exactly the valid providers, so a new provider cannot be left out of it silently.
var ErrProviderColumnsMismatch = errors.New(
	"support-matrix provider columns do not match the valid providers",
)

const (
	supportedCell              = "✅"
	unsupportedCell            = "❌"
	pendingLiveValidationMark  = "🧪"
	pendingLiveValidationNotes = "🧪 Implemented, but not yet validated on a live cluster."
)

// supportMatrixProviderColumns is the order the support matrix lists providers in: the local
// providers first, then the cloud providers.
func supportMatrixProviderColumns() []v1alpha1.Provider {
	return []v1alpha1.Provider{
		v1alpha1.ProviderDocker,
		v1alpha1.ProviderKubernetes,
		v1alpha1.ProviderHetzner,
		v1alpha1.ProviderOmni,
		v1alpha1.ProviderAWS,
		v1alpha1.ProviderGCP,
		v1alpha1.ProviderAzure,
	}
}

// supportMatrixDistributionLabel returns the row label of a distribution, naming the tool that
// runs it where that helps a reader recognise it.
func supportMatrixDistributionLabel(distribution v1alpha1.Distribution) (string, bool) {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return "Vanilla (Kind)", true
	case v1alpha1.DistributionK3s:
		return "K3s (K3d)", true
	case v1alpha1.DistributionTalos:
		return "Talos", true
	case v1alpha1.DistributionVCluster:
		return "VCluster (Vind)", true
	case v1alpha1.DistributionKWOK:
		return "KWOK (kwokctl)", true
	case v1alpha1.DistributionEKS:
		return "EKS", true
	case v1alpha1.DistributionGKE:
		return "GKE", true
	case v1alpha1.DistributionAKS:
		return "AKS", true
	default:
		return "", false
	}
}

// SupportMatrixCell renders one distribution × provider cell: supported or not, and marked
// when the combination is implemented but awaits live validation.
func SupportMatrixCell(supported, awaitingLiveValidation bool) string {
	if !supported {
		return unsupportedCell
	}

	if awaitingLiveValidation {
		return supportedCell + pendingLiveValidationMark
	}

	return supportedCell
}

// Markers around the generated distribution × provider table in the support matrix page.
// The table sits inside the hand-written page, between these lines, so its authored notes
// stay next to it and every reader of the page source sees the table itself.
const (
	SupportMatrixBeginMarker = "{/* BEGIN GENERATED distribution × provider table: " +
		"go generate ./docs/... rewrites it from the distribution metadata. DO NOT EDIT */}"
	SupportMatrixEndMarker = "{/* END GENERATED distribution × provider table */}"
)

// ErrSupportMatrixMarkers is returned when the support matrix page does not hold exactly one
// begin marker followed by exactly one end marker.
var ErrSupportMatrixMarkers = errors.New(
	"support matrix page needs one begin marker followed by one end marker",
)

// RenderDistributionProviderTable renders the distribution × provider table of the support
// matrix. Every cell comes from the distribution metadata KSail validates a configuration
// against, so the table cannot drift from the combinations KSail accepts. It fails when a
// distribution has no row label or the provider columns are not exactly the valid providers.
func RenderDistributionProviderTable() (string, error) {
	columns := supportMatrixProviderColumns()

	if !sameProviders(columns, v1alpha1.ValidProviders()) {
		return "", fmt.Errorf(
			"%w: columns %v, valid providers %v",
			ErrProviderColumnsMismatch, columns, v1alpha1.ValidProviders(),
		)
	}

	var builder strings.Builder

	builder.WriteString("| Distribution |")

	for _, provider := range columns {
		fmt.Fprintf(&builder, " %s |", provider)
	}

	builder.WriteString("\n| --- |" + strings.Repeat(" --- |", len(columns)) + "\n")

	anyPending := false

	for _, distribution := range v1alpha1.ValidDistributions() {
		label, found := supportMatrixDistributionLabel(distribution)
		if !found {
			return "", fmt.Errorf("%w: %s", ErrUnlabelledDistribution, distribution)
		}

		builder.WriteString("| " + label + " |")

		for _, provider := range columns {
			supported := provider.ValidateForDistribution(distribution) == nil
			pending := supported && distribution.AwaitsLiveValidationOn(provider)
			anyPending = anyPending || pending

			builder.WriteString(" " + SupportMatrixCell(supported, pending) + " |")
		}

		builder.WriteString("\n")
	}

	if anyPending {
		builder.WriteString("\n" + pendingLiveValidationNotes + "\n")
	}

	return builder.String(), nil
}

// ReplaceDistributionProviderTable returns page with the region between the support matrix
// markers replaced by table, separated from both markers by a blank line.
func ReplaceDistributionProviderTable(page, table string) (string, error) {
	if strings.Count(page, SupportMatrixBeginMarker) != 1 ||
		strings.Count(page, SupportMatrixEndMarker) != 1 {
		return "", ErrSupportMatrixMarkers
	}

	before, rest, _ := strings.Cut(page, SupportMatrixBeginMarker)

	_, after, found := strings.Cut(rest, SupportMatrixEndMarker)
	if !found {
		return "", ErrSupportMatrixMarkers
	}

	return before + SupportMatrixBeginMarker + "\n\n" + strings.TrimRight(table, "\n") + "\n\n" +
		SupportMatrixEndMarker + after, nil
}

// sameProviders reports whether both lists hold the same providers, each exactly once.
func sameProviders(left, right []v1alpha1.Provider) bool {
	sortedLeft := slices.Clone(left)
	sortedRight := slices.Clone(right)

	slices.Sort(sortedLeft)
	slices.Sort(sortedRight)

	return slices.Equal(sortedLeft, sortedRight)
}
