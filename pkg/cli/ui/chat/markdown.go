package chat

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
)

// listLevelIndent is the indentation depth for list items.
const listLevelIndent = 2

// createRenderer creates a glamour renderer with a static dark style.
// This avoids terminal queries that can cause escape sequences to be captured as input.
func createRenderer(width int) *glamour.TermRenderer {
	style := defaultMarkdownStyle()

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStyles(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}

	return renderer
}

// defaultMarkdownStyle returns a static dark style configuration for glamour.
// Uses a completely static style definition to avoid any terminal queries.
func defaultMarkdownStyle() ansi.StyleConfig { //nolint:funlen // pure struct literal definition
	return ansi.StyleConfig{
		Document: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				BlockPrefix: "",
				BlockSuffix: "",
			},
			Margin: uintPtr(0),
		},
		Heading: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new("39"),
				Bold:  new(true),
			},
		},
		H1: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "# ",
			},
		},
		H2: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "## ",
			},
		},
		H3: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Prefix: "### ",
			},
		},
		Paragraph: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{},
			Margin:         uintPtr(0),
		},
		List: ansi.StyleList{
			LevelIndent: listLevelIndent,
		},
		Item: ansi.StylePrimitive{
			BlockPrefix: "• ",
		},
		Enumeration: ansi.StylePrimitive{
			BlockPrefix: ". ",
		},
		Code: ansi.StyleBlock{
			StylePrimitive: ansi.StylePrimitive{
				Color: new("203"),
			},
		},
		CodeBlock: ansi.StyleCodeBlock{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{
					Color: new("244"),
				},
				Margin: uintPtr(1),
			},
			Chroma: &ansi.Chroma{
				Text: ansi.StylePrimitive{
					Color: new("#d0d0d0"),
				},
				Keyword: ansi.StylePrimitive{
					Color: new("#00afff"),
				},
				Name: ansi.StylePrimitive{
					Color: new("#87d7ff"),
				},
				LiteralString: ansi.StylePrimitive{
					Color: new("#5fd75f"),
				},
				LiteralNumber: ansi.StylePrimitive{
					Color: new("#d7005f"),
				},
				Comment: ansi.StylePrimitive{
					Color: new("#626262"),
				},
			},
		},
		Table: ansi.StyleTable{
			StyleBlock: ansi.StyleBlock{
				StylePrimitive: ansi.StylePrimitive{},
			},
			CenterSeparator: new("│"),
			ColumnSeparator: new("│"),
			RowSeparator:    new("─"),
		},
		Emph: ansi.StylePrimitive{
			Italic: new(true),
		},
		Strong: ansi.StylePrimitive{
			Bold: new(true),
		},
		Link: ansi.StylePrimitive{
			Color:     new("39"),
			Underline: new(true),
		},
		LinkText: ansi.StylePrimitive{
			Color: new("45"),
		},
	}
}

// renderMarkdownWithRenderer renders markdown using the provided renderer.
func renderMarkdownWithRenderer(renderer *glamour.TermRenderer, content string) string {
	if renderer == nil {
		return content
	}

	out, err := renderer.Render(content)
	if err != nil {
		return content
	}
	// Trim trailing whitespace that glamour adds
	return strings.TrimRight(out, "\n")
}

// uintPtr returns a pointer to the given uint value.
func uintPtr(u uint) *uint { return new(u) }
