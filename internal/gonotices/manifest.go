package gonotices

import (
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"strings"
)

const manifestSchemaVersion = 2

type sourceFact struct {
	Module      string       `json:"module"`
	Version     string       `json:"version"`
	Checksum    string       `json:"checksum"`
	Replacement *replacement `json:"replacement,omitempty"`
	URL         string       `json:"url"`
}

type materialFact struct {
	Kind        string   `json:"kind"`
	SHA256      string   `json:"sha256"`
	Raw         []string `json:"raw,omitempty"`
	Effective   []string `json:"effective,omitempty"`
	Disposition string   `json:"disposition,omitempty"`
}

type materialAssociation struct {
	Path     string `json:"path"`
	Material string `json:"material"`
}

type packageFact struct {
	Name      string                `json:"name"`
	Source    string                `json:"source"`
	Directory string                `json:"directory"`
	Raw       []string              `json:"raw"`
	Effective []string              `json:"effective"`
	Files     []string              `json:"files"`
	Materials []materialAssociation `json:"materials"`
}

type compactManifest struct {
	Schema     int                     `json:"schema"`
	Scope      string                  `json:"scope"`
	Target     string                  `json:"target"`
	TextSHA256 string                  `json:"textSha256"`
	Inputs     map[string]string       `json:"inputs"`
	Sources    map[string]sourceFact   `json:"sources"`
	Materials  map[string]materialFact `json:"materials"`
	Packages   []packageFact           `json:"packages"`
	Toolchain  *toolchainRecord        `json:"toolchain,omitempty"`
}

func sourceIdentity(record component) sourceFact {
	return sourceFact{
		record.Module,
		record.Version,
		record.Checksum,
		record.Replacement,
		record.Source,
	}
}

func materialIdentity(record material) materialFact {
	return materialFact{
		record.Kind,
		record.SHA256,
		record.Raw,
		record.Effective,
		record.Disposition,
	}
}

func factReference(prefix string, value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", ErrIncomplete
	}

	return prefix + digest(data)[:16], nil
}

func encodeManifest(metadata manifest) ([]byte, error) {
	wire := compactManifest{
		Schema:     manifestSchemaVersion,
		Scope:      metadata.Scope,
		Target:     metadata.Target,
		TextSHA256: metadata.TextSHA256,
		Inputs:     metadata.Inputs,
		Sources:    map[string]sourceFact{},
		Materials:  map[string]materialFact{},
		Toolchain:  metadata.Toolchain,
	}
	for _, record := range metadata.Components {
		pkg, err := wire.compactPackage(record)
		if err != nil {
			return nil, err
		}

		wire.Packages = append(wire.Packages, pkg)
	}

	data, err := json.Marshal(wire)
	if err != nil {
		return nil, ErrIncomplete
	}

	return append(data, '\n'), nil
}

func (wire *compactManifest) compactPackage(record component) (packageFact, error) {
	reference, err := wire.addSourceFact(sourceIdentity(record))
	if err != nil {
		return packageFact{}, err
	}

	pkg := packageFact{
		Name:      record.Package,
		Source:    reference,
		Directory: record.Directory,
		Raw:       record.Raw,
		Effective: record.Effective,
	}

	pkg.Files, err = compactPackageFiles(record.Directory, record.Files)
	if err != nil {
		return packageFact{}, err
	}

	for _, entry := range record.Materials {
		materialReference, materialErr := wire.addMaterialFact(materialIdentity(entry))
		if materialErr != nil {
			return packageFact{}, materialErr
		}

		pkg.Materials = append(pkg.Materials, materialAssociation{entry.Path, materialReference})
	}

	return pkg, nil
}

func compactPackageFiles(directory string, files []string) ([]string, error) {
	selected := make([]string, 0, len(files))
	for _, file := range files {
		relative, err := filepath.Rel(filepath.FromSlash(directory), filepath.FromSlash(file))
		if err != nil || !materialPath(filepath.ToSlash(relative)) {
			return nil, ErrIncomplete
		}

		selected = append(selected, filepath.ToSlash(relative))
	}

	slices.Sort(selected)

	return slices.Compact(selected), nil
}

func (wire *compactManifest) addSourceFact(source sourceFact) (string, error) {
	reference, err := factReference("s", source)
	if err != nil {
		return "", err
	}

	previous, exists := wire.Sources[reference]
	if exists && !sameFact(previous, source) {
		return "", ErrIncomplete
	}

	wire.Sources[reference] = source

	return reference, nil
}

func (wire *compactManifest) addMaterialFact(fact materialFact) (string, error) {
	reference, err := factReference("m", fact)
	if err != nil {
		return "", err
	}

	previous, exists := wire.Materials[reference]
	if exists && !sameFact(previous, fact) {
		return "", ErrIncomplete
	}

	wire.Materials[reference] = fact

	return reference, nil
}

func sameFact(left, right any) bool {
	first, firstErr := json.Marshal(left)
	second, secondErr := json.Marshal(right)

	return firstErr == nil && secondErr == nil && bytes.Equal(first, second)
}

func decodeManifest(data []byte) (manifest, error) {
	wire, err := parseCompactManifest(data)
	if err != nil {
		return manifest{}, err
	}

	if !validSourceFacts(wire.Sources) || !validMaterialFacts(wire.Materials) {
		return manifest{}, ErrIncomplete
	}

	metadata := manifest{
		Schema:     manifestSchemaVersion,
		Scope:      wire.Scope,
		Target:     wire.Target,
		TextSHA256: wire.TextSHA256,
		Inputs:     wire.Inputs,
		Toolchain:  wire.Toolchain,
	}
	usedSources, usedMaterials := map[string]bool{}, map[string]bool{}
	previousName := ""

	for _, pkg := range wire.Packages {
		source, exists := wire.Sources[pkg.Source]
		if !exists || !validPackageFact(pkg, source, previousName) {
			return manifest{}, ErrIncomplete
		}

		record, expandErr := expandPackageFact(pkg, source, wire.Materials, usedMaterials)
		if expandErr != nil {
			return manifest{}, expandErr
		}

		previousName = pkg.Name
		usedSources[pkg.Source] = true

		metadata.Components = append(metadata.Components, record)
	}

	if len(usedSources) != len(wire.Sources) || len(usedMaterials) != len(wire.Materials) {
		return manifest{}, ErrIncomplete
	}

	return metadata, nil
}

func parseCompactManifest(data []byte) (compactManifest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var wire compactManifest
	if decoder.Decode(&wire) != nil || decoder.Decode(new(any)) != io.EOF ||
		wire.Schema != manifestSchemaVersion ||
		len(wire.Packages) == 0 {
		return compactManifest{}, ErrIncomplete
	}

	canonical, err := json.Marshal(wire)
	if err != nil || !bytes.Equal(append(canonical, '\n'), data) {
		return compactManifest{}, ErrIncomplete
	}

	return wire, nil
}

func validSourceFacts(sources map[string]sourceFact) bool {
	for reference, source := range sources {
		expected, err := factReference("s", source)
		if err != nil || reference != expected || source.Module == "" || source.Version == "" ||
			source.URL == "" {
			return false
		}
	}

	return true
}

func validMaterialFacts(materials map[string]materialFact) bool {
	for reference, entry := range materials {
		expected, err := factReference("m", entry)
		if err != nil || reference != expected || !sha256Hex(entry.SHA256) ||
			!validMaterialKind(entry.Kind) {
			return false
		}
	}

	return true
}

func validPackageFact(pkg packageFact, source sourceFact, previousName string) bool {
	return validPackageName(pkg.Name, source.Module, previousName) &&
		(pkg.Directory == "." || materialPath(pkg.Directory)) && len(pkg.Raw) != 0 &&
		len(pkg.Effective) != 0 && !slices.Contains(pkg.Effective, "Unknown")
}

func validPackageName(name, module, previous string) bool {
	return name != "" && name > previous && (name == module || strings.HasPrefix(name, module+"/"))
}

func expandPackageFact(
	pkg packageFact,
	source sourceFact,
	materials map[string]materialFact,
	used map[string]bool,
) (component, error) {
	record := component{
		Package:     pkg.Name,
		Directory:   pkg.Directory,
		Module:      source.Module,
		Version:     source.Version,
		Checksum:    source.Checksum,
		Replacement: source.Replacement,
		Source:      source.URL,
		Raw:         pkg.Raw,
		Effective:   pkg.Effective,
	}

	var err error

	record.Files, err = expandPackageFiles(pkg.Directory, pkg.Files)
	if err != nil {
		return component{}, err
	}

	record.Materials, err = expandMaterialAssociations(source.URL, pkg.Materials, materials, used)
	if err != nil {
		return component{}, err
	}

	return record, nil
}

func expandPackageFiles(directory string, files []string) ([]string, error) {
	result := make([]string, 0, len(files))

	previous := ""
	for _, file := range files {
		if !materialPath(file) || file <= previous {
			return nil, ErrIncomplete
		}

		previous = file
		result = append(result, filepath.ToSlash(filepath.Join(directory, file)))
	}

	return result, nil
}

func expandMaterialAssociations(
	source string,
	associations []materialAssociation,
	materials map[string]materialFact,
	used map[string]bool,
) ([]material, error) {
	if len(associations) == 0 {
		return nil, ErrIncomplete
	}

	result := make([]material, 0, len(associations))
	previous := ""

	for _, association := range associations {
		fact, exists := materials[association.Material]
		if !exists || !materialPath(association.Path) || association.Path <= previous {
			return nil, ErrIncomplete
		}

		previous = association.Path
		used[association.Material] = true
		result = append(
			result,
			material{
				Source:      source,
				Path:        association.Path,
				Kind:        fact.Kind,
				SHA256:      fact.SHA256,
				Raw:         fact.Raw,
				Effective:   fact.Effective,
				Disposition: fact.Disposition,
			},
		)
	}

	return result, nil
}

func validMaterialKind(kind string) bool {
	return slices.Contains(
		[]string{
			licenseMaterialKind,
			reviewedDeclarationKind,
			"notice",
			supplementalDeclarationKind,
			copyrightHeaderKind,
			assemblyHeaderKind,
			compiledHeaderKind,
		},
		kind,
	)
}
