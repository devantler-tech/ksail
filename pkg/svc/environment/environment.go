package environment

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrInvalidRewrite is returned by RewriteOverlayFile when a rewrite is malformed
// (an empty Old/New, or a MetaFieldValue rewrite missing its Field).
var ErrInvalidRewrite = errors.New("invalid environment rewrite")

// RewriteKind classifies a structured environment-clone substitution.
type RewriteKind int

const (
	// MetaFieldValue rewrites the value of a YAML mapping field (the cluster-meta
	// ConfigMap's cluster_name or provider) when the field's value equals Old
	// exactly. It is matched as a "key: value" line, so the same text under a
	// different key, inside a comment, or as a substring is never touched.
	MetaFieldValue RewriteKind = iota
	// PathSegment rewrites the environment name where it functions as a path or
	// filename component — the clusters/<env>/ directory and the ksail.<env>.yaml
	// config file. In a relative path it matches whole '/'- and '.'-delimited
	// tokens; in file contents it matches only after the clusters/ prefix and on a
	// trailing boundary, so substrings such as "localhost" are never touched.
	PathSegment
)

// Rewrite describes one structured substitution applied when cloning a
// cluster environment overlay from a source environment to a new one.
type Rewrite struct {
	// Kind selects how Old/New are matched and applied.
	Kind RewriteKind
	// Field is the YAML mapping key whose value is rewritten (MetaFieldValue only).
	Field string
	// Old is the source value (MetaFieldValue) or environment name (PathSegment).
	Old string
	// New is the destination value or environment name.
	New string
}

// validate reports whether the rewrite is well-formed, rejecting an unknown Kind
// so a malformed rewrite fails with ErrInvalidRewrite rather than silently
// no-opping through RewriteOverlayFile's switch.
func (r Rewrite) validate() error {
	if r.Old == "" || r.New == "" {
		return fmt.Errorf("%w: Old and New must be non-empty", ErrInvalidRewrite)
	}

	switch r.Kind {
	case MetaFieldValue:
		if r.Field == "" {
			return fmt.Errorf("%w: MetaFieldValue requires a Field", ErrInvalidRewrite)
		}
	case PathSegment:
		// No extra fields required.
	default:
		return fmt.Errorf("%w: unknown Kind %d", ErrInvalidRewrite, r.Kind)
	}

	return nil
}

// DeriveRewrites returns the structured substitutions that turn a clone
// of the srcName environment overlay into the dstName environment. cluster_name is
// always repointed and the clusters/<env>/ path segment always swapped; the
// provider is repointed only when dstProvider is non-empty and differs from
// srcProvider (an empty dstProvider inherits the source environment's provider).
func DeriveRewrites(srcName, dstName, dstProvider, srcProvider string) []Rewrite {
	rewrites := []Rewrite{
		{Kind: MetaFieldValue, Field: "cluster_name", Old: srcName, New: dstName},
		{Kind: PathSegment, Old: srcName, New: dstName},
	}

	if dstProvider != "" && dstProvider != srcProvider {
		rewrites = append(rewrites, Rewrite{
			Kind:  MetaFieldValue,
			Field: "provider",
			Old:   srcProvider,
			New:   dstProvider,
		})
	}

	return rewrites
}

// RewriteOverlayFile applies the rewrites to one cloned overlay file, returning the
// file's new repository-relative path and its new contents. Only the structured
// locations the rewrites target are changed; every other byte is preserved, so a
// cloned kustomization's replacements: block and base wiring survive intact. It
// returns ErrInvalidRewrite if any rewrite is malformed.
func RewriteOverlayFile(
	relPath, content string,
	rewrites []Rewrite,
) (string, string, error) {
	newRelPath := relPath
	newContent := content

	for _, rewrite := range rewrites {
		err := rewrite.validate()
		if err != nil {
			return "", "", err
		}

		switch rewrite.Kind {
		case MetaFieldValue:
			newContent = rewriteFieldValue(newContent, rewrite.Field, rewrite.Old, rewrite.New)
		case PathSegment:
			newRelPath = rewritePathTokens(newRelPath, rewrite.Old, rewrite.New)
			newContent = rewriteClustersPath(newContent, rewrite.Old, rewrite.New)
		}
	}

	return newRelPath, newContent, nil
}

// rewriteFieldValue rewrites every "field: old" mapping line in content to
// "field: new", preserving indentation, quoting, and any trailing comment.
func rewriteFieldValue(content, field, old, replacement string) string {
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		if rewritten, ok := rewriteFieldLine(line, field, old, replacement); ok {
			lines[index] = rewritten
		}
	}

	return strings.Join(lines, "\n")
}

// rewriteFieldLine rewrites a single "field: old" line to "field: new" when the
// line's scalar value matches old exactly, returning the new line and true. Any
// other line is returned unchanged with false.
func rewriteFieldLine(line, field, old, replacement string) (string, bool) {
	indentLen := len(line) - len(strings.TrimLeft(line, " \t"))
	indent, rest := line[:indentLen], line[indentLen:]

	prefix := field + ":"
	if !strings.HasPrefix(rest, prefix) {
		return line, false
	}

	after := rest[len(prefix):]
	if after == "" || (after[0] != ' ' && after[0] != '\t') {
		return line, false
	}

	leadLen := len(after) - len(strings.TrimLeft(after, " \t"))
	lead, valueRegion := after[:leadLen], after[leadLen:]

	if valueRegion == "" {
		return line, false
	}

	scalar, tail := scanScalar(valueRegion)

	unquoted, quote := unquoteScalar(scalar)
	if unquoted != old {
		return line, false
	}

	return indent + prefix + lead + quote + replacement + quote + tail, true
}

// scanScalar splits a value region into its leading scalar token (quoted or bare)
// and the trailing remainder (whitespace and/or a "# ..." comment).
func scanScalar(region string) (string, string) {
	if region[0] == '\'' || region[0] == '"' {
		quote := region[0]
		for index := 1; index < len(region); index++ {
			if region[index] == quote {
				return region[:index+1], region[index+1:]
			}
		}

		return region, ""
	}

	if index := strings.IndexAny(region, " \t"); index != -1 {
		return region[:index], region[index:]
	}

	return region, ""
}

// unquoteScalar strips a single pair of matching surrounding quotes, returning the
// inner text and the quote character used (empty when the scalar is unquoted).
func unquoteScalar(scalar string) (string, string) {
	if len(scalar) >= 2 &&
		(scalar[0] == '\'' || scalar[0] == '"') &&
		scalar[len(scalar)-1] == scalar[0] {
		return scalar[1 : len(scalar)-1], string(scalar[0])
	}

	return scalar, ""
}

// rewritePathTokens replaces every whole '/'- and '.'-delimited token equal to old
// with replacement, so the clusters/<env>/ directory segment and the
// ksail.<env>.yaml filename are repointed without touching substrings.
func rewritePathTokens(path, old, replacement string) string {
	slashParts := strings.Split(path, "/")
	for sIndex, slashPart := range slashParts {
		dotParts := strings.Split(slashPart, ".")
		for dIndex, dotPart := range dotParts {
			if dotPart == old {
				dotParts[dIndex] = replacement
			}
		}

		slashParts[sIndex] = strings.Join(dotParts, ".")
	}

	return strings.Join(slashParts, "/")
}

// rewriteClustersPath rewrites a "clusters/<old>" path reference in file contents
// to "clusters/<new>", matching only on a trailing boundary so "clusters/<old>x"
// is left untouched. It rewrites only the code portion of each line, leaving a
// trailing "# ..." comment unchanged so a documentary reference such as
// "# see clusters/<old>" is preserved.
func rewriteClustersPath(content, old, replacement string) string {
	pattern := regexp.MustCompile(`clusters/` + regexp.QuoteMeta(old) + `([/"'\s]|$)`)
	lines := strings.Split(content, "\n")

	for index, line := range lines {
		code, comment := splitLineComment(line)
		lines[index] = pattern.ReplaceAllString(code, "clusters/"+replacement+"$1") + comment
	}

	return strings.Join(lines, "\n")
}

// splitLineComment splits a line into its code portion and a trailing YAML
// comment — a "#" at the line start or preceded by whitespace, outside any
// quoted scalar. A line with no such comment returns the whole line and "".
func splitLineComment(line string) (string, string) {
	var quote byte

	for index := range len(line) {
		char := line[index]

		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '#' && commentPrecededByBoundary(line, index):
			return line[:index], line[index:]
		}
	}

	return line, ""
}

// commentPrecededByBoundary reports whether the "#" at index begins a YAML
// comment, i.e. it is at the line start or preceded by whitespace.
func commentPrecededByBoundary(line string, index int) bool {
	return index == 0 || line[index-1] == ' ' || line[index-1] == '\t'
}
