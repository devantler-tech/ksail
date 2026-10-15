package codeqlprofile

import (
	"bufio"
	"io"
	"path/filepath"
	"regexp"
	"strings"
)

var extractedLine = regexp.MustCompile(
	`^(?:[0-9]{4}/[0-9]{2}/[0-9]{2} [0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)? )?` +
		`Done extracting (.+\.go) \([0-9]+ms\)$`,
)

// CountExtractedFiles counts distinct repository Go paths reported complete by
// the native extractor. A log completion precedes closing its TRAP writer, so
// these observations never certify finalized database coverage.
func CountExtractedFiles(sourceRoot string, input io.Reader) (uint64, error) {
	if !filepath.IsAbs(sourceRoot) {
		return 0, errMeasurement
	}

	seen := make(map[string]bool)
	scanner := bufio.NewScanner(input)

	const initialBuffer, maximumLine = 64 << 10, 1 << 20
	scanner.Buffer(make([]byte, initialBuffer), maximumLine)

	for scanner.Scan() {
		match := extractedLine.FindStringSubmatch(scanner.Text())
		if match == nil || !filepath.IsAbs(match[1]) {
			continue
		}

		path, err := filepath.Rel(sourceRoot, match[1])
		if err != nil || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) ||
			filepath.IsAbs(path) {
			continue
		}

		seen[path] = true
	}

	if scanner.Err() != nil {
		return 0, errMeasurement
	}

	return uint64(len(seen)), nil
}
