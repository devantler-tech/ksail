// Package gonotices prepares recipient material for the independently admitted Go graph.
package gonotices

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/devantler-tech/ksail/v7/internal/moduleintegrity"
	"golang.org/x/mod/module"
	"golang.org/x/mod/sumdb/dirhash"
)

// ErrIncomplete means a notice payload lacks source-bound recipient evidence.
var ErrIncomplete = errors.New("go recipient evidence is incomplete")

// Module keeps requested and actual replacement identities separate.
type Module struct {
	Path, Version, Sum, Directory string
	Main                          bool
	Replace                       *Module
}

// Package describes one independently joined, admitted source package.
type Package struct {
	ImportPath, Directory, LicenseFile  string
	UpstreamTermsURL                    string
	GoFiles, EmbedFiles, Raw, Effective []string
	AssemblyFiles                       []string
	EvidenceFiles                       []string
	OtherFiles                          []string
	Declarations                        []Declaration
	Module                              Module
}

// Input contains the exact target graph and repository-owned source inputs.
type Input struct {
	Root, Target string
	Packages     []Package
	Toolchain    *Toolchain
}

// Payload contains reviewable recipient text and its machine evidence manifest.
type Payload struct {
	Text, Manifest []byte
}

type material struct {
	Source      string   `json:"source"`
	Path        string   `json:"path"`
	Kind        string   `json:"kind"`
	SHA256      string   `json:"sha256"`
	Raw         []string `json:"raw,omitempty"`
	Effective   []string `json:"effective,omitempty"`
	Disposition string   `json:"disposition,omitempty"`
}

// Declaration retains the independently evaluated terms of a supplemental file.
type Declaration struct {
	Path           string
	Raw, Effective []string
	Role           string
}

type component struct {
	Directory   string       `json:"-"`
	Package     string       `json:"package"`
	Module      string       `json:"module"`
	Version     string       `json:"version"`
	Checksum    string       `json:"checksum"`
	Raw         []string     `json:"raw"`
	Effective   []string     `json:"effective"`
	Source      string       `json:"source"`
	Replacement *replacement `json:"replacement,omitempty"`
	Files       []string     `json:"files"`
	Materials   []material   `json:"materials"`
}

type replacement struct {
	Kind               string `json:"kind"`
	Module             string `json:"module,omitempty"`
	Version            string `json:"version,omitempty"`
	Checksum           string `json:"checksum,omitempty"`
	RepositoryRelative string `json:"repositoryRelative,omitempty"`
	ContentHash        string `json:"contentHash,omitempty"`
}

type manifest struct {
	Schema     int               `json:"schema"`
	Scope      string            `json:"scope"`
	Target     string            `json:"target"`
	TextSHA256 string            `json:"textSha256"`
	Inputs     map[string]string `json:"inputs"`
	Components []component       `json:"components"`
	Toolchain  *toolchainRecord  `json:"toolchain,omitempty"`
}

const (
	maxMaterialBytes       = 1 << 20
	primaryLicenseFilename = "LICENSE"
)

// Collector reuses only authenticated immutable source and individual file
// evidence. Every target supplies and joins its own complete package/file set.
type Collector struct {
	mutex    sync.Mutex
	verified map[string]bool
	text     map[string][]byte
	local    map[string]string
}

// Build derives recipient evidence from the exact selected source files. Input
// membership and license admission are independently checked by the caller.
func Build(input Input) (Payload, error) {
	return new(Collector).Build(input)
}

// Build collects one independently loaded target. Results never alias cached
// evidence. Local source identities are recomputed for each target.
func (collector *Collector) Build(input Input) (Payload, error) {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()

	collector.initializeCaches()

	if !filepath.IsAbs(input.Root) || len(input.Packages) == 0 || !validTarget(input.Target) {
		return Payload{}, ErrIncomplete
	}

	metadata, err := targetManifest(input)
	if err != nil {
		return Payload{}, err
	}

	pool := map[string][]byte{}

	err = collector.collectTargetToolchain(input.Toolchain, &metadata, pool)
	if err != nil {
		return Payload{}, err
	}

	err = collector.collectTargetPackages(input, &metadata, pool)
	if err != nil {
		return Payload{}, err
	}

	text, err := renderTargetText(metadata, pool)
	if err != nil {
		return Payload{}, err
	}

	metadata.TextSHA256 = digest(text)

	encoded, err := encodeManifest(metadata)
	if err != nil {
		return Payload{}, fmt.Errorf("encode Go recipient evidence: %w", err)
	}

	return Payload{Text: text, Manifest: encoded}, nil
}

func (collector *Collector) initializeCaches() {
	if collector.verified == nil {
		collector.verified = map[string]bool{}
	}

	if collector.text == nil {
		collector.text = map[string][]byte{}
	}

	collector.local = map[string]string{}
}

func targetManifest(input Input) (manifest, error) {
	metadata := manifest{
		Schema: manifestSchemaVersion,
		Target: input.Target,
		Scope:  "Admitted third-party Go source packages; other artifact contents require separate evidence",
		Inputs: map[string]string{},
	}
	for _, name := range sourceInputNames() {
		data, err := readMaterial(input.Root, name)
		if err != nil {
			return manifest{}, err
		}

		metadata.Inputs[name] = digest(data)
	}

	return metadata, nil
}

func (collector *Collector) collectTargetToolchain(
	selected *Toolchain,
	metadata *manifest,
	pool map[string][]byte,
) error {
	if selected == nil {
		return nil
	}

	record, materials, err := collector.collectToolchain(*selected)
	if err != nil {
		return err
	}

	metadata.Toolchain = &record

	maps.Copy(pool, materials)

	return nil
}

func (collector *Collector) collectTargetPackages(
	input Input,
	metadata *manifest,
	pool map[string][]byte,
) error {
	packages := slices.Clone(input.Packages)
	slices.SortFunc(
		packages,
		func(left, right Package) int { return strings.Compare(left.ImportPath, right.ImportPath) },
	)

	for index, pkg := range packages {
		if pkg.ImportPath == "" || index > 0 && packages[index-1].ImportPath == pkg.ImportPath {
			return ErrIncomplete
		}

		if pkg.Module.Main {
			continue
		}

		record, materials, err := collector.collectComponent(input.Root, pkg)
		if err != nil {
			return fmt.Errorf("%w: recipient material for %s", err, pkg.ImportPath)
		}

		metadata.Components = append(metadata.Components, record)

		maps.Copy(pool, materials)
	}

	if len(metadata.Components) == 0 {
		return ErrIncomplete
	}

	return nil
}

func renderTargetText(metadata manifest, pool map[string][]byte) ([]byte, error) {
	var text bytes.Buffer
	text.WriteString(
		"THIRD-PARTY GO NOTICES\n\nTarget: " + metadata.Target + "\n" + metadata.Scope + "\n\n",
	)
	text.WriteString(
		"For a release-source:<path> locator, read the exact source= commit from GoNotices/release.txt " +
			"and obtain that public source at https://github.com/devantler-tech/ksail/tree/<source-commit>/<path>. " +
			"This locates repository modifications; it does not grant additional rights.\n\n",
	)

	if metadata.Toolchain != nil {
		fmt.Fprintf(
			&text,
			"Go runtime and standard library toolchain: %s\nLicense SHA-256: %s\n",
			metadata.Toolchain.Version,
			metadata.Toolchain.LicenseSHA256,
		)
	}

	err := renderComponentText(&text, metadata.Components)
	if err != nil {
		return nil, err
	}

	writePooledText(&text, pool)

	return text.Bytes(), nil
}

func writePooledText(text *bytes.Buffer, pool map[string][]byte) {
	hashes := make([]string, 0, len(pool))
	for hash := range pool {
		hashes = append(hashes, hash)
	}

	slices.Sort(hashes)

	for _, hash := range hashes {
		fmt.Fprintf(text, "--- Recipient material SHA-256 %s ---\n", hash)
		text.Write(pool[hash])
		text.WriteString("\n\n")
	}
}

func validTarget(target string) bool {
	return slices.Contains(recipientTargets(), target)
}

func (collector *Collector) collectComponent(
	root string,
	pkg Package,
) (component, map[string][]byte, error) {
	if !validNoticePackage(pkg) {
		return component{}, nil, ErrIncomplete
	}

	selected, source, replaced, err := collector.selectedSource(root, pkg.Module)
	if err != nil {
		return component{}, nil, err
	}

	source, err = componentSourceLocator(pkg, source)
	if err != nil {
		return component{}, nil, err
	}

	result, err := componentMetadata(selected, pkg, source, replaced)
	if err != nil {
		return component{}, nil, err
	}

	files, err := componentMaterials(selected.Directory, pkg)
	if err != nil {
		return component{}, nil, err
	}

	identity := selected.Directory + "\x00" + selected.Path + "\x00" + selected.Version + "\x00" + selected.Sum
	if replaced != nil && replaced.Kind == repositoryReplacementKind {
		identity += "\x00" + replaced.ContentHash
	}

	contents, err := collector.componentMaterialContents(
		identity,
		selected.Directory,
		pkg,
		files,
		&result,
	)
	if err != nil {
		return component{}, nil, err
	}

	return result, contents, nil
}

func validNoticePackage(pkg Package) bool {
	return len(pkg.Effective) != 0 && !slices.Contains(pkg.Effective, "Unknown") &&
		(pkg.ImportPath == pkg.Module.Path || strings.HasPrefix(pkg.ImportPath, pkg.Module.Path+"/"))
}

func componentSourceLocator(pkg Package, source string) (string, error) {
	if !slices.Contains(pkg.Effective, "LicenseRef-Loft-KSail-Binary-Permission") {
		return source, nil
	}

	terms, err := url.Parse(pkg.UpstreamTermsURL)
	if err != nil {
		return "", ErrIncomplete
	}

	if !validUpstreamTermsURL(terms) || pkg.Module.Path != "github.com/loft-sh/admin-apis" {
		return "", ErrIncomplete
	}

	return pkg.UpstreamTermsURL, nil
}

func validUpstreamTermsURL(terms *url.URL) bool {
	return terms.Scheme == "https" && terms.Host == "github.com" && terms.User == nil &&
		terms.RawQuery == "" &&
		terms.Fragment == "" &&
		strings.HasPrefix(terms.Path, "/loft-sh/admin-apis/blob/") &&
		strings.HasSuffix(terms.Path, "/LICENSE")
}

func componentMetadata(
	selected Module,
	pkg Package,
	source string,
	replaced *replacement,
) (component, error) {
	directory, err := scopedRelative(selected.Directory, pkg.Directory)
	if err != nil {
		return component{}, err
	}

	result := component{
		Package:     pkg.ImportPath,
		Module:      pkg.Module.Path,
		Version:     pkg.Module.Version,
		Checksum:    pkg.Module.Sum,
		Replacement: replaced,
		Raw:         slices.Clone(pkg.Raw),
		Effective:   slices.Clone(pkg.Effective),
		Source:      source,
		Directory:   filepath.ToSlash(directory),
	}

	result.Files, err = componentSelectedFiles(selected.Directory, pkg)
	if err != nil {
		return component{}, err
	}

	return result, nil
}

func componentSelectedFiles(directory string, pkg Package) ([]string, error) {
	names := append(slices.Clone(pkg.GoFiles), pkg.AssemblyFiles...)
	names = append(names, pkg.OtherFiles...)
	names = append(names, pkg.EmbedFiles...)

	files := make([]string, 0, len(names))
	for _, name := range names {
		relative, err := scopedRelative(directory, filepath.Join(pkg.Directory, name))
		if err != nil {
			return nil, err
		}

		files = append(files, filepath.ToSlash(relative))
	}

	slices.Sort(files)

	return files, nil
}

func (collector *Collector) componentMaterialContents(
	identity, directory string,
	pkg Package,
	files []materialFile,
	result *component,
) (map[string][]byte, error) {
	contents := map[string][]byte{}

	for _, file := range files {
		data, err := collector.materialText(identity, directory, file)
		if err != nil || !publicMaterial(data) {
			return nil, ErrIncomplete
		}

		if len(data) == 0 {
			continue
		}

		entry := componentMaterialEntry(result.Source, file, data, pkg.Declarations)

		result.Materials = append(result.Materials, entry)
		if entry.Disposition != excludedDisposition {
			contents[digest(data)] = data
		}
	}

	return contents, nil
}

func componentMaterialEntry(
	source string,
	file materialFile,
	data []byte,
	declarations []Declaration,
) material {
	entry := material{Source: source, Path: file.path, Kind: file.kind, SHA256: digest(data)}
	for _, declaration := range declarations {
		if declaration.Path != file.path {
			continue
		}

		entry.Raw = slices.Clone(declaration.Raw)
		entry.Effective = slices.Clone(declaration.Effective)
		entry.Disposition = declaration.Role
	}

	return entry
}

func (collector *Collector) selectedSource(
	root string,
	declared Module,
) (Module, string, *replacement, error) {
	selected := declared
	if declared.Replace != nil {
		selected = *declared.Replace
	}

	if declared.Replace != nil && selected.Version == "" {
		source, replaced, err := collector.repositorySource(root, declared, selected)

		return selected, source, replaced, err
	}

	source, err := collector.publishedSource(selected)
	if err != nil {
		return Module{}, "", nil, err
	}

	var replaced *replacement
	if declared.Replace != nil {
		replaced = &replacement{
			Kind:     "module",
			Module:   selected.Path,
			Version:  selected.Version,
			Checksum: selected.Sum,
		}
	}

	return selected, source, replaced, nil
}

func (collector *Collector) repositorySource(
	root string,
	declared, selected Module,
) (string, *replacement, error) {
	relative, err := filepath.Rel(root, selected.Directory)
	if err != nil || !filepath.IsLocal(relative) ||
		!strings.HasPrefix(filepath.ToSlash(relative), "third_party/") ||
		!ordinaryDirectories(root, relative) {
		return "", nil, ErrIncomplete
	}

	key := selected.Directory + "\x00" + declared.Path + "@" + declared.Version

	contentHash, exists := collector.local[key]
	if !exists {
		contentHash, err = repositoryContentHash(
			selected.Directory,
			declared.Path+"@"+declared.Version,
		)
		if err != nil {
			return "", nil, err
		}

		collector.local[key] = contentHash
	}

	return "release-source:" + filepath.ToSlash(
		relative,
	), &replacement{
		Kind:               repositoryReplacementKind,
		RepositoryRelative: filepath.ToSlash(relative),
		ContentHash:        contentHash,
	}, nil
}

func (collector *Collector) publishedSource(selected Module) (string, error) {
	escapedPath, pathErr := module.EscapePath(selected.Path)

	escapedVersion, versionErr := module.EscapeVersion(selected.Version)
	if pathErr != nil || versionErr != nil || selected.Version == "" || selected.Sum == "" {
		return "", ErrIncomplete
	}

	key := selected.Directory + "\x00" + selected.Path + "@" + selected.Version + "\x00" + selected.Sum
	if !collector.verified[key] {
		err := moduleintegrity.Verify(
			selected.Directory,
			selected.Path+"@"+selected.Version,
			selected.Sum,
		)
		if err != nil {
			return "", ErrIncomplete
		}

		collector.verified[key] = true
	}

	return "https://proxy.golang.org/" + escapedPath + "/@v/" + escapedVersion + ".zip", nil
}

func (collector *Collector) materialText(
	identity, directory string,
	file materialFile,
) ([]byte, error) {
	key := identity + "\x00" + file.kind + "\x00" + file.path
	if data, exists := collector.text[key]; exists {
		return slices.Clone(data), nil
	}

	var (
		data []byte
		err  error
	)

	switch file.kind {
	case copyrightHeaderKind, assemblyHeaderKind, compiledHeaderKind:
		data, err = readSourceCopyright(directory, file.path, file.kind == copyrightHeaderKind)
	default:
		data, err = readMaterial(directory, file.path)
	}

	if err != nil {
		return nil, err
	}

	collector.text[key] = slices.Clone(data)

	return data, nil
}

type materialFile struct{ path, kind string }

// SupplementalPaths discovers declarations only along the selected package and
// embedded-file ancestor chains. It does not enumerate unlinked subtrees.
func SupplementalPaths(pkg Package) ([]string, error) {
	selected := pkg.Module
	if selected.Replace != nil {
		selected = *selected.Replace
	}

	files, err := componentMaterials(selected.Directory, pkg)
	if err != nil {
		return nil, err
	}

	var paths []string

	for _, file := range files {
		if file.kind == supplementalDeclarationKind {
			paths = append(paths, file.path)
		}
	}

	return paths, nil
}

const (
	licenseMaterialKind         = licenseDisposition
	reviewedDeclarationKind     = "reviewed-declaration"
	supplementalDeclarationKind = "supplemental-declaration"
	copyrightHeaderKind         = "copyright-header"
	assemblyHeaderKind          = "assembly-copyright-header"
	compiledHeaderKind          = "compiled-source-copyright-header"
)

type sourceFileGroup struct {
	names []string
	kind  string
}

func componentMaterials(directory string, pkg Package) ([]materialFile, error) {
	files, err := componentDeclarations(directory, pkg)
	if err != nil {
		return nil, err
	}

	packageDir, err := scopedRelative(directory, pkg.Directory)
	if err != nil {
		return nil, err
	}

	err = collectNoticeAncestors(directory, packageDir, files)
	if err != nil {
		return nil, err
	}

	_, err = collectHeaderFileGroups(
		packageDir,
		[]sourceFileGroup{
			{pkg.GoFiles, copyrightHeaderKind},
			{pkg.AssemblyFiles, assemblyHeaderKind},
			{pkg.OtherFiles, compiledHeaderKind},
		},
		files,
	)
	if err != nil {
		return nil, err
	}

	_, err = collectEmbeddedNotices(directory, packageDir, pkg.EmbedFiles, files)
	if err != nil {
		return nil, err
	}

	return sortedMaterialFiles(files), nil
}

func componentDeclarations(directory string, pkg Package) (map[string]string, error) {
	files := map[string]string{}

	if pkg.LicenseFile != "" {
		license, err := scopedRelative(directory, pkg.LicenseFile)
		if err != nil {
			return nil, err
		}

		files[license] = licenseMaterialKind
	}

	for _, evidence := range pkg.EvidenceFiles {
		if !materialPath(evidence) {
			return nil, ErrIncomplete
		}

		if _, exists := files[evidence]; !exists {
			files[evidence] = reviewedDeclarationKind
		}
	}

	if len(files) == 0 {
		return nil, ErrIncomplete
	}

	return files, nil
}

func collectHeaderFileGroups(
	relative string,
	groups []sourceFileGroup,
	files map[string]string,
) ([]string, error) {
	var selected []string

	for _, group := range groups {
		for _, name := range group.names {
			if !filepath.IsLocal(name) {
				return nil, ErrIncomplete
			}

			path := filepath.Join(relative, name)
			files[path] = group.kind
			selected = append(selected, filepath.ToSlash(path))
		}
	}

	return selected, nil
}

func collectEmbeddedNotices(
	directory, relative string,
	names []string,
	files map[string]string,
) ([]string, error) {
	var selected []string

	for _, name := range names {
		if !filepath.IsLocal(name) {
			return nil, ErrIncomplete
		}

		path := filepath.Join(relative, name)

		file, err := openMaterial(directory, path)
		if err != nil {
			return nil, ErrIncomplete
		}

		if file.Close() != nil {
			return nil, ErrIncomplete
		}

		err = collectNoticeAncestors(directory, filepath.Dir(path), files)
		if err != nil {
			return nil, err
		}

		selected = append(selected, filepath.ToSlash(path))
	}

	return selected, nil
}

func sortedMaterialFiles(files map[string]string) []materialFile {
	result := make([]materialFile, 0, len(files))
	for name, kind := range files {
		result = append(result, materialFile{filepath.ToSlash(name), kind})
	}

	slices.SortFunc(
		result,
		func(left, right materialFile) int { return strings.Compare(left.path, right.path) },
	)

	return result
}

func collectNoticeAncestors(directory, relative string, files map[string]string) error {
	for current := relative; ; current = filepath.Dir(current) {
		entries, err := os.ReadDir(filepath.Join(directory, current))
		if err != nil {
			return ErrIncomplete
		}

		for _, entry := range entries {
			kind := noticeCandidateKind(entry)
			if kind == "" {
				continue
			}

			name := filepath.Join(current, entry.Name())
			if _, exists := files[name]; !exists {
				files[name] = kind
			}
		}

		if current == "." {
			return nil
		}
	}
}

func noticeCandidateKind(entry fs.DirEntry) string {
	upper := strings.ToUpper(entry.Name())
	if entry.IsDir() || strings.HasSuffix(upper, ".GO") {
		return ""
	}

	for _, prefix := range []string{primaryLicenseFilename, "LICENCE", "COPYING", "UNLICENSE", "UNLICENCE"} {
		if strings.HasPrefix(upper, prefix) {
			return supplementalDeclarationKind
		}
	}

	if strings.HasPrefix(upper, "NOTICE") || strings.HasPrefix(upper, "COPYRIGHT") {
		return "notice"
	}

	return ""
}

func repositoryContentHash(directory, prefix string) (string, error) {
	err := filepath.WalkDir(directory, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || !entry.IsDir() && !entry.Type().IsRegular() {
			return ErrIncomplete
		}

		return nil
	})
	if err != nil {
		return "", ErrIncomplete
	}

	checksum, err := dirhash.HashDir(directory, prefix, dirhash.Hash1)
	if err != nil {
		return "", ErrIncomplete
	}

	return checksum, nil
}

func scopedRelative(directory, name string) (string, error) {
	relative, err := filepath.Rel(directory, name)
	if err != nil || !filepath.IsLocal(relative) {
		return "", ErrIncomplete
	}

	return relative, nil
}

func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func publicMaterial(data []byte) bool {
	for _, marker := range []string{
		"/Users/", "/private/tmp/", "/private/var/", "PRIVATE_CORRESPONDENCE", "LOFT-ADMIN-APIS-KSAIL-001",
	} {
		if bytes.Contains(data, []byte(marker)) {
			return false
		}
	}

	return true
}

func readMaterial(directory, name string) ([]byte, error) {
	file, err := openMaterial(directory, name)
	if err != nil {
		return nil, ErrIncomplete
	}

	data, readErr := io.ReadAll(io.LimitReader(file, maxMaterialBytes+1))

	closeErr := file.Close()
	if readErr != nil || closeErr != nil || len(data) == 0 || len(data) > maxMaterialBytes {
		return nil, ErrIncomplete
	}

	return data, nil
}

func readSourceCopyright(directory, name string, requirePackage bool) ([]byte, error) {
	file, err := openMaterial(directory, name)
	if err != nil {
		return nil, err
	}

	data, readErr := leadingCopyright(file, requirePackage)
	if errors.Join(readErr, file.Close()) != nil {
		return nil, ErrIncomplete
	}

	return data, nil
}

const (
	maxSourceHeaderBytes = 64 << 10
	initialHeaderBytes   = 4 << 10
)

type sourceHeaderNotice struct {
	group, selected bytes.Buffer
	blockComment    bool
	readBytes       int
}

func leadingCopyright(source io.Reader, requirePackage bool) ([]byte, error) {
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, initialHeaderBytes), maxSourceHeaderBytes)

	var notice sourceHeaderNotice
	for scanner.Scan() {
		complete, err := notice.consume(scanner.Text(), requirePackage)
		if err != nil {
			return nil, err
		}

		if complete {
			return notice.selected.Bytes(), nil
		}
	}

	if scanner.Err() != nil || requirePackage || notice.blockComment {
		return nil, ErrIncomplete
	}

	notice.flush()

	return notice.selected.Bytes(), nil
}

func (notice *sourceHeaderNotice) flush() {
	if relevantSourceNotice(notice.group.Bytes()) {
		notice.selected.Write(notice.group.Bytes())
		notice.selected.WriteString("\n")
	}

	notice.group.Reset()
}

func (notice *sourceHeaderNotice) consume(line string, requirePackage bool) (bool, error) {
	code := strings.TrimSpace(strings.TrimPrefix(line, "\ufeff"))

	code, notice.blockComment = uncommentedHeader(code, notice.blockComment)
	if code != "" {
		return notice.completeLine(line, code, requirePackage)
	}

	notice.group.WriteString(line + "\n")

	notice.readBytes += len(line) + 1
	if notice.readBytes > maxSourceHeaderBytes {
		return false, ErrIncomplete
	}

	if strings.TrimSpace(line) == "" && !notice.blockComment {
		notice.flush()
	}

	return false, nil
}

func (notice *sourceHeaderNotice) completeLine(
	line, code string,
	requirePackage bool,
) (bool, error) {
	if requirePackage && !packageClause(code) {
		return false, ErrIncomplete
	}

	prefix := strings.LastIndex(line, code)
	if prefix > 0 {
		notice.group.WriteString(line[:prefix])
	}

	notice.flush()

	return true, nil
}

func packageClause(code string) bool {
	return strings.HasPrefix(code, "package") && len(code) > len("package") &&
		unicode.IsSpace(rune(code[len("package")]))
}

func uncommentedHeader(code string, block bool) (string, bool) {
	for code != "" {
		switch {
		case block:
			end := strings.Index(code, "*/")
			if end < 0 {
				return "", true
			}

			code, block = strings.TrimSpace(code[end+2:]), false
		case strings.HasPrefix(code, "//"):
			return "", false
		case strings.HasPrefix(code, "/*"):
			code, block = strings.TrimSpace(code[2:]), true
		default:
			return code, block
		}
	}

	return code, block
}

func relevantSourceNotice(group []byte) bool {
	for line := range strings.SplitSeq(string(group), "\n") {
		line = strings.ToLower(strings.TrimLeft(line, " /*\t"))
		for _, prefix := range []string{
			"copyright ", "copyright(", "copyright\t", "copyright:", "©",
			"spdx-license-identifier:", "spdx-filecopyrighttext:",
		} {
			if strings.HasPrefix(line, prefix) {
				return true
			}
		}
	}

	return false
}

func ordinaryDirectories(root, relative string) bool {
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() {
		return false
	}

	current := root
	for part := range strings.SplitSeq(filepath.Clean(relative), string(filepath.Separator)) {
		current = filepath.Join(current, part)

		info, err = os.Lstat(current)
		if err != nil || !info.IsDir() {
			return false
		}
	}

	return true
}

func openMaterial(directory, name string) (*os.File, error) {
	if !filepath.IsLocal(name) || name == "." ||
		!ordinaryDirectories(directory, filepath.Dir(name)) {
		return nil, ErrIncomplete
	}

	root, err := os.OpenRoot(directory)
	if err != nil {
		return nil, ErrIncomplete
	}

	info, inspectErr := root.Lstat(name)
	if inspectErr != nil || !info.Mode().IsRegular() {
		_ = root.Close()

		return nil, ErrIncomplete
	}

	file, openErr := root.Open(name)

	closeErr := root.Close()
	if openErr != nil || closeErr != nil {
		if file != nil {
			_ = file.Close()
		}

		return nil, ErrIncomplete
	}

	return file, nil
}
