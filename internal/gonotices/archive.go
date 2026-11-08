package gonotices

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil/pathutil"
)

const (
	maxRecipientArchiveBytes int64 = 512 << 20
	maxArchiveExpandedBytes  int64 = 2 << 30
	maxArchiveMemberBytes          = 512 << 20
	maxArchiveIndexBytes     int64 = 8 << 20
	maxArchiveMembers              = 10_000
	maxArchivePathBytes            = 4096
	archiveCopyBufferBytes         = 32 << 10
	recipientZIPFormat             = "zip"
)

// VerifyArchive checks declared release layouts against independently verified
// recipient material. It never extracts or executes archive members.
func VerifyArchive(filename string, release Release, expected Payload) (err error) {
	layout, err := ReleaseLayout(release)
	if err != nil || filepath.Base(filename) != layout.Archive {
		return ErrIncomplete
	}

	binding, err := Binding(release, expected)
	if err != nil {
		return ErrIncomplete
	}

	file, err := openRecipientArchive(filename)
	if err != nil {
		return ErrIncomplete
	}

	defer func() {
		if file.Close() != nil {
			err = ErrIncomplete
		}
	}()

	state := archiveRecipientState{
		binary:  layout.Binary,
		seen:    make(map[string]bool),
		parents: make(map[string]bool),
		remaining: map[string][]byte{
			layout.Prefix + "THIRD_PARTY_NOTICES.txt": expected.Text,
			layout.Prefix + "manifest.json":           expected.Manifest,
			layout.Prefix + "release.txt":             binding,
		},
	}

	if layout.Format == recipientZIPFormat {
		err = state.verifyZIP(file)
	} else {
		err = state.verifyTarGzip(file)
	}

	if err != nil || !state.binaryFound || len(state.remaining) != 0 {
		return ErrIncomplete
	}

	return nil
}

func openRecipientArchive(filename string) (*os.File, error) {
	canonical, err := pathutil.EvalCanonicalPath(filename)
	if err != nil {
		return nil, ErrIncomplete
	}

	root, err := os.OpenRoot(filepath.Dir(canonical))
	if err != nil {
		return nil, ErrIncomplete
	}

	file, openErr := root.Open(filepath.Base(canonical))

	closeErr := root.Close()
	if openErr != nil || closeErr != nil {
		if file != nil {
			_ = file.Close()
		}

		return nil, ErrIncomplete
	}

	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maxRecipientArchiveBytes {
		_ = file.Close()

		return nil, ErrIncomplete
	}

	return file, nil
}

type archiveRecipientState struct {
	binary      string
	binaryFound bool
	remaining   map[string][]byte
	seen        map[string]bool
	parents     map[string]bool
	expanded    int64
}

// archiveIndexReader bounds standard-library ZIP index parsing, before it can
// allocate entries from an arbitrarily large directory. Body reads follow their
// separate member and expansion limits after the index has been checked.
type archiveIndexReader struct {
	file      *os.File
	remaining int64
}

func (reader *archiveIndexReader) ReadAt(data []byte, offset int64) (int, error) {
	if reader.remaining >= 0 && int64(len(data)) > reader.remaining {
		return 0, ErrIncomplete
	}

	count, err := reader.file.ReadAt(data, offset)
	if reader.remaining >= 0 {
		reader.remaining -= int64(count)
	}

	if errors.Is(err, io.EOF) {
		return count, io.EOF
	}

	if err != nil {
		return count, ErrIncomplete
	}

	return count, nil
}

func (state *archiveRecipientState) verifyZIP(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return ErrIncomplete
	}

	index := &archiveIndexReader{file: file, remaining: maxArchiveIndexBytes}

	reader, err := zip.NewReader(index, info.Size())
	if err != nil || len(reader.File) > maxArchiveMembers {
		return ErrIncomplete
	}

	index.remaining = -1

	for _, member := range reader.File {
		if member.UncompressedSize64 > maxArchiveMemberBytes {
			return ErrIncomplete
		}

		stream, openErr := member.Open()
		if openErr != nil {
			return ErrIncomplete
		}

		readErr := state.readMember(
			member.Name,
			int64(member.UncompressedSize64),
			member.Mode(),
			stream,
		)
		if errors.Join(readErr, stream.Close()) != nil {
			return ErrIncomplete
		}
	}

	return nil
}

func (state *archiveRecipientState) verifyTarGzip(file *os.File) (err error) {
	buffer := bufio.NewReader(file)

	compressed, err := gzip.NewReader(buffer)
	if err != nil {
		return ErrIncomplete
	}

	compressed.Multistream(false)
	defer func() {
		if compressed.Close() != nil {
			err = ErrIncomplete
		}
	}()

	limited := &io.LimitedReader{R: compressed, N: maxArchiveExpandedBytes + 1}

	readErr := state.readTar(tar.NewReader(limited))
	if readErr != nil {
		return readErr
	}

	if zeroPadding(limited) != nil || limited.N == 0 {
		return ErrIncomplete
	}

	_, trailingErr := buffer.ReadByte()
	if !errors.Is(trailingErr, io.EOF) {
		return ErrIncomplete
	}

	return nil
}

func (state *archiveRecipientState) readTar(reader *tar.Reader) error {
	for {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			return nil
		}

		if nextErr != nil || header.Size < 0 {
			return ErrIncomplete
		}

		var mode fs.FileMode

		switch header.Typeflag {
		case tar.TypeReg:
		case tar.TypeDir:
			mode = fs.ModeDir
		default:
			return ErrIncomplete
		}

		readErr := state.readMember(header.Name, header.Size, mode, reader)
		if readErr != nil {
			return readErr
		}
	}
}

func (state *archiveRecipientState) readMember(
	name string,
	size int64,
	mode fs.FileMode,
	reader io.Reader,
) error {
	if size < 0 || size > maxArchiveMemberBytes || size > maxArchiveExpandedBytes-state.expanded {
		return ErrIncomplete
	}

	nameErr := state.registerName(name, mode)
	if nameErr != nil {
		return nameErr
	}

	state.expanded += size

	if mode.IsDir() {
		_, required := state.remaining[name]
		if size != 0 || name == state.binary || required {
			return ErrIncomplete
		}

		return nil
	}

	return state.readRegularMember(name, size, reader)
}

func validArchiveMemberName(name string, mode fs.FileMode) bool {
	canonical := strings.TrimSuffix(name, "/")

	return canonical != "." && fs.ValidPath(canonical) && len(name) <= maxArchivePathBytes &&
		!strings.ContainsFunc(
			name,
			windowsInvalidNameRune,
		) && portableArchiveComponents(canonical) &&
		(mode.IsRegular() || mode.IsDir())
}

// windowsInvalidNameRune reports characters Windows refuses in a path component.
// PowerShell Expand-Archive stops at such a member, which can leave later
// required material unextracted, so every recipient archive refuses them.
func windowsInvalidNameRune(char rune) bool {
	return char < ' ' || strings.ContainsRune(`\:<>"|?*`, char)
}

// Windows shell extraction does not preserve trailing dots or spaces. Refuse
// those components instead of permitting a second spelling of required material.
func portableArchiveComponents(name string) bool {
	for component := range strings.SplitSeq(name, "/") {
		if strings.TrimRight(component, " .") != component {
			return false
		}
	}

	return true
}

func (state *archiveRecipientState) registerName(name string, mode fs.FileMode) error {
	if !validArchiveMemberName(name, mode) {
		return ErrIncomplete
	}

	canonical := strings.ToLower(strings.TrimSuffix(name, "/"))
	if _, duplicate := state.seen[canonical]; duplicate || len(state.seen) >= maxArchiveMembers {
		return ErrIncomplete
	}

	if !mode.IsDir() && state.parents[canonical] {
		return ErrIncomplete
	}

	for parent := path.Dir(canonical); parent != "."; parent = path.Dir(parent) {
		if directory, exists := state.seen[parent]; exists && !directory {
			return ErrIncomplete
		}

		state.parents[parent] = true
	}

	state.seen[canonical] = mode.IsDir()

	return nil
}

func (state *archiveRecipientState) readRegularMember(
	name string,
	size int64,
	reader io.Reader,
) error {
	expected, required := state.remaining[name]
	if required && size != int64(len(expected)) {
		return ErrIncomplete
	}

	if name == state.binary {
		state.binaryFound = size != 0
	}

	if !required {
		count, err := io.Copy(io.Discard, io.LimitReader(reader, size+1))
		if err != nil || count != size {
			return ErrIncomplete
		}

		return nil
	}

	data, err := io.ReadAll(io.LimitReader(reader, int64(len(expected))+1))
	if err != nil || !bytes.Equal(data, expected) {
		return ErrIncomplete
	}

	delete(state.remaining, name)

	return nil
}

// zeroPadding consumes the gzip trailer as well as optional tar record padding;
// Close alone does not validate a gzip checksum. Another archive is not padding.
func zeroPadding(reader io.Reader) error {
	buffer := make([]byte, archiveCopyBufferBytes)
	for {
		count, err := reader.Read(buffer)
		if bytes.Count(buffer[:count], []byte{0}) != count {
			return ErrIncomplete
		}

		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return ErrIncomplete
		}
	}
}
