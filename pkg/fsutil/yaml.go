package fsutil

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"

	yamlio "k8s.io/apimachinery/pkg/util/yaml"
)

// YAML document helpers.

// SplitYAMLDocuments splits multi-document YAML content into individual
// documents using a YAML-aware reader, so document separators ("---") are
// recognized correctly regardless of position, trailing whitespace, or
// carriage returns — and a "---" line nested inside a block scalar does NOT
// split the document (the naive strings.Split("\n---") variants this replaces
// got that wrong).
//
// This is the lossy variant intended for read-only detection callers: each
// returned document is whitespace-trimmed and empty documents are dropped.
// It is therefore NOT suitable for round-trip rewriting where leading
// whitespace and exact separators must be preserved — those callers must keep
// their own preserving splitter.
//
// If the reader fails to parse the stream as multi-document YAML, the original
// (trimmed) content is returned as a single document so detection callers still
// see the bytes rather than silently losing them.
func SplitYAMLDocuments(data []byte) [][]byte {
	reader := yamlio.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))

	docs := make([][]byte, 0)

	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return singleTrimmedDocument(data)
		}

		doc = stripLeadingDocumentMarker(bytes.TrimSpace(doc))
		if len(doc) == 0 {
			continue
		}

		docs = append(docs, doc)
	}

	if len(docs) == 0 {
		return singleTrimmedDocument(data)
	}

	return docs
}

// singleTrimmedDocument returns data as a single trimmed document, or an empty
// slice when the content is blank.
func singleTrimmedDocument(data []byte) [][]byte {
	trimmed := stripLeadingDocumentMarker(bytes.TrimSpace(data))
	if len(trimmed) == 0 {
		return [][]byte{}
	}

	return [][]byte{trimmed}
}

// stripLeadingDocumentMarker removes a leading "---" document-start marker line
// from an already-trimmed document, so callers see the document body without
// the separator the YAML reader keeps attached to a document that opened the
// stream. A bare "---" yields an empty document.
func stripLeadingDocumentMarker(doc []byte) []byte {
	if !bytes.HasPrefix(doc, []byte("---")) {
		return doc
	}

	rest := doc[len("---"):]
	if len(rest) == 0 {
		return nil
	}

	// Only treat "---" as a marker when it is its own line (followed by a
	// newline), never when it is the start of a longer token like "----".
	if rest[0] == '\n' || rest[0] == '\r' {
		return bytes.TrimSpace(rest)
	}

	return doc
}

// IsYAMLFile reports whether path has a YAML extension (".yaml" or ".yml"),
// matched case-insensitively. Only the extension is inspected; the file does
// not need to exist.
func IsYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))

	return ext == ".yaml" || ext == ".yml"
}
