// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

package docs_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/docs"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
)

// loadAPIFieldDocs loads doc comments from the v1alpha1 API package sources.
func loadAPIFieldDocs(t *testing.T) docs.FieldDocs {
	t.Helper()

	fieldDocs, err := docs.LoadFieldDocs("../pkg/apis/cluster/v1alpha1")
	if err != nil {
		t.Fatalf("loading API field docs: %v", err)
	}

	return fieldDocs
}

func TestLoadFieldDocs(t *testing.T) {
	t.Parallel()

	fieldDocs := loadAPIFieldDocs(t)

	if doc := fieldDocs["OptionsHetzner"]["Location"]; !strings.Contains(
		doc,
		"datacenter location",
	) {
		t.Errorf("OptionsHetzner.Location doc comment not extracted, got %q", doc)
	}

	if doc := fieldDocs["OptionsOmni"][""]; !strings.Contains(doc, "Sidero Omni") {
		t.Errorf("OptionsOmni type-level doc comment not extracted, got %q", doc)
	}
}

func TestRenderFieldTableOptionsHetzner(t *testing.T) {
	t.Parallel()

	table := docs.RenderFieldTable(
		reflect.TypeFor[v1alpha1.OptionsHetzner](), "", loadAPIFieldDocs(t),
	)

	expectations := []string{
		"| `controlPlaneServerType` |",
		"`cx23`",
		"| `workerPublicIPv4` | boolean |",
		"| `fallbackLocations` | []string |",
	}
	for _, want := range expectations {
		if !strings.Contains(table, want) {
			t.Errorf("OptionsHetzner table missing %q", want)
		}
	}

	if strings.Contains(table, "NodeAutoscalerEnabled") {
		t.Error("runtime-only json:\"-\" fields must not be documented")
	}
}

func TestRenderTypeSectionsProviderSpec(t *testing.T) {
	t.Parallel()

	sections := docs.RenderTypeSections(
		"spec.provider", reflect.TypeFor[v1alpha1.ProviderSpec](), loadAPIFieldDocs(t),
	)

	headings := []string{
		"### spec.provider (ProviderSpec)",
		"### spec.provider.hetzner (OptionsHetzner)",
		"### spec.provider.omni (OptionsOmni)",
		"### spec.provider.aws (OptionsAWS)",
		"### spec.provider.kubernetes (OptionsKubernetes)",
	}
	for _, heading := range headings {
		if !strings.Contains(sections, heading) {
			t.Errorf("provider sections missing heading %q", heading)
		}
	}

	if !strings.Contains(sections, "| `machineClass` |") {
		t.Error("provider sections missing OptionsOmni machineClass row")
	}
}

func TestRenderTypeSectionsAutoscaler(t *testing.T) {
	t.Parallel()

	sections := docs.RenderTypeSections(
		"spec.cluster.autoscaler",
		reflect.TypeFor[v1alpha1.AutoscalerConfig](),
		loadAPIFieldDocs(t),
	)

	headings := []string{
		"### spec.cluster.autoscaler (AutoscalerConfig)",
		"### spec.cluster.autoscaler.node (NodeAutoscalerConfig)",
		"### spec.cluster.autoscaler.node.pools[] (NodePool)",
		"### spec.cluster.autoscaler.node.pools[].taints[] (NodePoolTaint)",
	}
	for _, heading := range headings {
		if !strings.Contains(sections, heading) {
			t.Errorf("autoscaler sections missing heading %q", heading)
		}
	}
}

func TestConfigReferenceProviderSections(t *testing.T) {
	t.Parallel()

	text := readOrSkip(t, "src/content/docs/configuration/declarative-configuration.mdx")
	if !strings.Contains(text, "### spec.provider (ProviderSpec)") {
		t.Skip("config reference not yet regenerated (run go generate ./docs/...)")
	}

	expectations := []string{
		"### spec.provider.hetzner (OptionsHetzner)",
		"### spec.provider.omni (OptionsOmni)",
		"### spec.cluster.autoscaler (AutoscalerConfig)",
		"| `serverLimit` |",
		"| `machineClass` |",
	}
	for _, want := range expectations {
		if !strings.Contains(text, want) {
			t.Errorf("config reference missing %q", want)
		}
	}
}
