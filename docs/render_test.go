// Copyright (c) KSail contributors. All rights reserved.
// Licensed under the PolyForm Shield License 1.0.0. See LICENSE in the project root.

package docs_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/docs"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
)

// testVersion is the version string used for test root commands.
const testVersion = "test"

func TestRenderCLIIndexPage(t *testing.T) {
	t.Parallel()

	root := cmd.NewRootCmd(testVersion, "", "")
	page := docs.RenderCLIIndexPage(root)

	if !strings.Contains(page, "auto-generated") {
		t.Error("index page missing auto-generation tag")
	}

	if !strings.Contains(page, "## Command Groups") {
		t.Error("index page missing Command Groups section")
	}

	for _, sub := range root.Commands() {
		if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
			continue
		}

		link := "(/cli-flags/" + sub.Name() + "/" + sub.Name() + "-root/)"
		if !strings.Contains(page, link) {
			t.Errorf("index page missing command group link %q", link)
		}
	}

	if strings.Contains(page, "/cli-flags/operator/") {
		t.Error("index page must not link hidden commands")
	}
}

func TestCLIFlagsIndexInSync(t *testing.T) {
	t.Parallel()

	text := readOrSkip(t, "src/content/docs/cli-flags/index.mdx")
	if !strings.Contains(text, docs.AutoGenTag) {
		t.Skip("index.mdx not yet regenerated (run go generate ./docs/...)")
	}

	root := cmd.NewRootCmd(testVersion, "", "")
	if expected := docs.RenderCLIIndexPage(root); text != expected {
		t.Error("cli-flags/index.mdx is out of sync; run go generate ./docs/...")
	}
}

func TestRenderMCPToolsPartial(t *testing.T) {
	t.Parallel()

	partial := docs.RenderMCPToolsPartial(cmd.NewRootCmd(testVersion, "", ""))
	tools := toolgen.GenerateTools(cmd.NewRootCmd(testVersion, "", ""), toolgen.DefaultOptions())

	if len(tools) == 0 {
		t.Fatal("toolgen returned no tools")
	}

	countPhrase := fmt.Sprintf("**%d tools**", len(tools))
	if !strings.Contains(partial, countPhrase) {
		t.Errorf("partial missing tool count phrase %q", countPhrase)
	}

	for _, tool := range tools {
		if !strings.Contains(partial, "### "+tool.Name) {
			t.Errorf("partial missing tool section %q", tool.Name)
		}

		for name := range tool.Subcommands {
			if !strings.Contains(partial, "| `"+name+"` |") {
				t.Errorf("partial missing %s subcommand row %q", tool.Name, name)
			}
		}
	}
}

func TestMCPToolsPartialInSync(t *testing.T) {
	t.Parallel()

	text := readOrSkip(t, "src/partials/mcp-available-tools.mdx")

	root := cmd.NewRootCmd(testVersion, "", "")
	if expected := docs.RenderMCPToolsPartial(root); text != expected {
		t.Error("src/partials/mcp-available-tools.mdx is out of sync; run go generate ./docs/...")
	}
}
