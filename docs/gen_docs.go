// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

//go:build ignore

// gen_docs.go generates reference documentation from Go source:
//
//   - CLI flags pages under docs/src/content/docs/cli-flags/ (one MDX per
//     command, plus the generated index.mdx landing page)
//   - Configuration reference at docs/src/content/docs/configuration/declarative-configuration.mdx
//   - MCP tool catalog partial at docs/src/partials/mcp-available-tools.mdx
//     (imported by src/content/docs/mcp.mdx)
//
// Rendering helpers live in the importable docs package (render.go,
// fielddocs.go) so they stay unit-testable; this file wires them to the
// filesystem.
//
// Usage:
//
//	go run gen_docs.go gen_docs_prose.go
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/devantler-tech/ksail/v7/docs"
	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd"
	"github.com/spf13/cobra"
)

const (
	cliDocsDir      = "src/content/docs/cli-flags"
	configDoc       = "src/content/docs/configuration/declarative-configuration.mdx"
	mcpToolsPartial = "src/partials/mcp-available-tools.mdx"
	apiTypesDir     = "../pkg/apis/cluster/v1alpha1"

	dirPermissions  = 0o750
	filePermissions = 0o600
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	// --- CLI flags ---
	root := cmd.NewRootCmd("dev", "", "")

	if err := generateCLIDocs(root); err != nil {
		return fmt.Errorf("generating CLI docs: %w", err)
	}

	// --- MCP tool catalog partial ---
	// Rendered from a fresh root: generateCLIDocs walks the shared tree and
	// cobra mutates it along the way (help command/flag injection), which
	// changes what toolgen sees versus a freshly constructed command.
	if err := generateMCPToolsPartial(cmd.NewRootCmd("dev", "", "")); err != nil {
		return fmt.Errorf("generating MCP tools partial: %w", err)
	}

	// --- Configuration reference ---
	if err := generateConfigReference(); err != nil {
		return fmt.Errorf("generating config reference: %w", err)
	}

	return nil
}

// ── CLI flags documentation ────────────────────────────────────────────────

// generateCLIDocs recreates the cli-flags directory (including the generated
// index.mdx landing page) and walks the Cobra command tree to produce one MDX
// page per command.
func generateCLIDocs(root *cobra.Command) error {
	// Clean directory.
	if err := os.RemoveAll(cliDocsDir); err != nil {
		return fmt.Errorf("removing %s: %w", cliDocsDir, err)
	}

	if err := os.MkdirAll(cliDocsDir, dirPermissions); err != nil {
		return fmt.Errorf("creating %s: %w", cliDocsDir, err)
	}

	// Generate index.mdx (root help + command groups).
	indexPath := filepath.Join(cliDocsDir, "index.mdx")

	indexContent := docs.RenderCLIIndexPage(root)
	if err := os.WriteFile(indexPath, []byte(indexContent), filePermissions); err != nil {
		return fmt.Errorf("writing index.mdx: %w", err)
	}

	// Walk command tree.
	var count int

	for _, sub := range root.Commands() {
		if docs.HiddenFromDocs(sub) {
			continue
		}

		n, err := generateCommandDocs(sub, nil)
		if err != nil {
			return err
		}

		count += n
	}

	fmt.Printf("gen_docs: wrote %d CLI flags pages (+ index.mdx)\n", count)

	return nil
}

// generateMCPToolsPartial writes the generated MCP tool catalog partial that
// src/content/docs/mcp.mdx imports into its "Available Tools" section.
func generateMCPToolsPartial(root *cobra.Command) error {
	if err := os.MkdirAll(filepath.Dir(mcpToolsPartial), dirPermissions); err != nil {
		return fmt.Errorf("creating %s dir: %w", mcpToolsPartial, err)
	}

	content := docs.RenderMCPToolsPartial(root)
	if err := os.WriteFile(mcpToolsPartial, []byte(content), filePermissions); err != nil {
		return fmt.Errorf("writing %s: %w", mcpToolsPartial, err)
	}

	fmt.Printf("gen_docs: wrote %s (%d bytes)\n", mcpToolsPartial, len(content))

	return nil
}

// generateCommandDocs recursively generates MDX pages for a command.
// parentNames carries the accumulated command path (e.g. ["cluster"]).
func generateCommandDocs(c *cobra.Command, parentNames []string) (int, error) {
	names := append(parentNames, c.Name()) //nolint:gocritic // intentional append to copy

	var subs []*cobra.Command

	for _, sub := range c.Commands() {
		if docs.HiddenFromDocs(sub) {
			continue
		}

		subs = append(subs, sub)
	}

	// Directory for this level is the first name (top-level group).
	groupDir := filepath.Join(cliDocsDir, names[0])
	if err := os.MkdirAll(groupDir, dirPermissions); err != nil {
		return 0, fmt.Errorf("creating %s: %w", groupDir, err)
	}

	// Write root page for this command.
	prefix := strings.Join(names, "-")
	rootFile := filepath.Join(groupDir, prefix+"-root.mdx")
	title := "ksail " + strings.Join(names, " ")
	description := c.Short

	if err := writeCLIPage(rootFile, title, description, c); err != nil {
		return 0, err
	}

	count := 1

	// Recurse into subcommands.
	for _, sub := range subs {
		// Check if this subcommand itself has children.
		var hasChildren bool

		for _, grandchild := range sub.Commands() {
			if !docs.HiddenFromDocs(grandchild) {
				hasChildren = true
				break
			}
		}

		if hasChildren {
			n, err := generateCommandDocs(sub, names)
			if err != nil {
				return 0, err
			}

			count += n
		} else {
			leafFile := filepath.Join(groupDir, prefix+"-"+sub.Name()+".mdx")
			leafTitle := "ksail " + strings.Join(names, " ") + " " + sub.Name()

			if err := writeCLIPage(leafFile, leafTitle, sub.Short, sub); err != nil {
				return 0, err
			}

			count++
		}
	}

	return count, nil
}

// writeCLIPage writes a single Starlight MDX page for a Cobra command.
func writeCLIPage(path, title, description string, c *cobra.Command) error {
	var b strings.Builder

	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("title: %q\n", title))
	b.WriteString(fmt.Sprintf("description: %q\n", description))
	b.WriteString("---\n\n")
	b.WriteString(docs.AutoGenTag)
	b.WriteString("\n\n```text\n")

	helpText := docs.CaptureHelp(c)
	b.WriteString(docs.SanitizeHelpText(helpText))
	b.WriteString("\n```\n")

	if footer, ok := commandSeeAlso[c.CommandPath()]; ok {
		b.WriteString(footer)
	}

	return os.WriteFile(path, []byte(b.String()), filePermissions)
}

// commandSeeAlso maps Cobra command paths (from c.CommandPath()) to "See also"
// footers appended after the auto-generated help text block.
var commandSeeAlso = map[string]string{
	"ksail workload watch": "\n> For image rebuild automation and local↔remote traffic bridging, see the [Companion Tools](/integrations/companion-tools/) guide.\n",
}

// ── Configuration reference ────────────────────────────────────────────────

// generateConfigReference builds the declarative-configuration.mdx page from
// prose constants (in gen_docs_prose.go), reflection on v1alpha1 types, and
// Go doc comments parsed from the API package sources.
func generateConfigReference() error {
	fieldDocs, err := docs.LoadFieldDocs(apiTypesDir)
	if err != nil {
		return fmt.Errorf("loading API field docs: %w", err)
	}

	var b strings.Builder

	// Frontmatter and intro.
	b.WriteString(configFrontmatter)
	b.WriteString("\n\n")
	b.WriteString(docs.AutoGenTag)
	b.WriteString("\n\n")
	b.WriteString(configIntroProse)
	b.WriteString("\n\n")

	// Environment variable expansion section.
	b.WriteString(configEnvVarProse)
	b.WriteString("\n\n")

	// Examples.
	b.WriteString(configMinimalExampleProse)
	b.WriteString("\n\n")
	b.WriteString(configCompleteExampleProse)
	b.WriteString("\n\n")

	// Configuration Reference section — generated from types.
	b.WriteString("## Configuration Reference\n\n")
	generateConfigReferenceTables(&b, fieldDocs)

	// Distribution configuration section.
	b.WriteString(configDistributionConfigProse)
	b.WriteString("\n\n")

	// Schema support section.
	b.WriteString(configSchemaProse)
	b.WriteString("\n")

	if err := os.MkdirAll(filepath.Dir(configDoc), dirPermissions); err != nil {
		return fmt.Errorf("creating %s dir: %w", configDoc, err)
	}

	if err := os.WriteFile(configDoc, []byte(b.String()), filePermissions); err != nil {
		return fmt.Errorf("writing %s: %w", configDoc, err)
	}

	fmt.Printf("gen_docs: wrote %s (%d bytes)\n", configDoc, b.Len())

	return nil
}

// generateConfigReferenceTables writes Markdown tables for Cluster, Spec,
// ClusterSpec, ProviderSpec, AutoscalerConfig, WorkloadSpec, ChatSpec and
// Connection types, plus enum sections.
func generateConfigReferenceTables(b *strings.Builder, fieldDocs docs.FieldDocs) {
	// Top-Level Fields.
	b.WriteString("### Top-Level Fields\n\n")
	b.WriteString("| Field | Type | Required | Description |\n")
	b.WriteString("| ----- | ---- | -------- | ----------- |\n")
	b.WriteString("| `apiVersion` | string | Yes | Must be `ksail.io/v1alpha1` |\n")
	b.WriteString("| `kind` | string | Yes | Must be `Cluster` |\n")
	b.WriteString("| `spec` | object | Yes | Cluster and workload specification (see below) |\n\n")

	// spec.
	b.WriteString("### spec\n\n")
	b.WriteString(
		"The `spec` field is a `Spec` object that defines editor, cluster, and workload configuration.\n\n",
	)
	b.WriteString(docs.RenderFieldTable(reflect.TypeOf(v1alpha1.Spec{}), "", fieldDocs))

	// spec.editor description.
	b.WriteString("### spec.editor\n\n")
	b.WriteString("Editor command for interactive workflows (e.g., `code --wait`, `vim`). " +
		"Falls back to `SOPS_EDITOR`, `KUBE_EDITOR`, `EDITOR`, `VISUAL`, or system defaults.\n\n")

	// spec.cluster (ClusterSpec).
	b.WriteString("### spec.cluster (ClusterSpec)\n\n")
	b.WriteString(docs.RenderFieldTable(reflect.TypeOf(v1alpha1.ClusterSpec{}), "", fieldDocs))

	// Enum detail sections for cluster fields.
	generateEnumSection(
		b,
		"distribution",
		reflect.TypeOf(v1alpha1.Distribution("")),
		distributionDetails,
	)
	generateEnumSection(b, "provider", reflect.TypeOf(v1alpha1.Provider("")), providerDetails)

	b.WriteString(configDistributionProse)
	b.WriteString("\n\n")

	// Connection.
	b.WriteString(configConnectionProse)
	b.WriteString("\n\n")

	generateEnumSection(b, "cni", reflect.TypeOf(v1alpha1.CNI("")), cniDetails)
	generateEnumSection(b, "csi", reflect.TypeOf(v1alpha1.CSI("")), csiDetails)
	generateEnumSection(
		b,
		"metricsServer",
		reflect.TypeOf(v1alpha1.MetricsServer("")),
		metricsServerDetails,
	)
	generateEnumSection(
		b,
		"certManager",
		reflect.TypeOf(v1alpha1.CertManager("")),
		certManagerDetails,
	)
	generateEnumSection(
		b,
		"policyEngine",
		reflect.TypeOf(v1alpha1.PolicyEngine("")),
		policyEngineDetails,
	)

	b.WriteString(configLocalRegistryProse)
	b.WriteString("\n\n")

	generateEnumSection(
		b,
		"gitOpsEngine",
		reflect.TypeOf(v1alpha1.GitOpsEngine("")),
		gitOpsEngineDetails,
	)

	b.WriteString(configDistToolOptions)
	b.WriteString("\n\n")

	// spec.cluster.autoscaler (AutoscalerConfig) and nested types.
	b.WriteString(docs.RenderTypeSections(
		"spec.cluster.autoscaler", reflect.TypeOf(v1alpha1.AutoscalerConfig{}), fieldDocs,
	))

	// spec.provider (ProviderSpec) and nested provider option types.
	b.WriteString(docs.RenderTypeSections(
		"spec.provider", reflect.TypeOf(v1alpha1.ProviderSpec{}), fieldDocs,
	))

	// spec.workload (WorkloadSpec).
	b.WriteString("### spec.workload (WorkloadSpec)\n\n")
	b.WriteString(docs.RenderFieldTable(reflect.TypeOf(v1alpha1.WorkloadSpec{}), "", fieldDocs))
	b.WriteString("\n")

	// spec.chat (ChatSpec).
	b.WriteString("### spec.chat (ChatSpec)\n\n")
	b.WriteString(docs.RenderFieldTable(reflect.TypeOf(v1alpha1.ChatSpec{}), "", fieldDocs))
	b.WriteString("\n")
}

// generateEnumSection writes a #### section documenting an enum field
// with its valid values extracted from the EnumValuer interface.
// When details prose is provided, the auto-generated bare values list is
// skipped — the details constant is expected to list values with descriptions.
func generateEnumSection(b *strings.Builder, fieldName string, t reflect.Type, details string) {
	b.WriteString(fmt.Sprintf("#### %s\n\n", fieldName))

	if details != "" {
		b.WriteString(details)
		b.WriteString("\n\n")
		return
	}

	// No prose provided — auto-generate a bare valid-values list.
	enumValuerType := reflect.TypeFor[v1alpha1.EnumValuer]()
	ptrType := reflect.PointerTo(t)

	if ptrType.Implements(enumValuerType) {
		zero := reflect.New(t)
		values := zero.Interface().(v1alpha1.EnumValuer).ValidValues()

		b.WriteString("**Valid values:**\n\n")

		for _, v := range values {
			b.WriteString(fmt.Sprintf("- `%s`", v))

			// Check if we have a default.
			if hasDefault, ok := zero.Interface().(interface{ Default() any }); ok {
				def := hasDefault.Default()
				if fmt.Sprint(def) == v {
					b.WriteString(" (default)")
				}
			}

			b.WriteString("\n")
		}
	}

	b.WriteString("\n\n")
}
