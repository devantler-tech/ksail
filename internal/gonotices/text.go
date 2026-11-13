package gonotices

import (
	"bytes"
	"fmt"
	"slices"
	"strings"
)

type textComponentGroup struct {
	source   sourceFact
	packages []component
}

func renderComponentText(text *bytes.Buffer, records []component) error {
	groups, err := groupTextComponents(records)
	if err != nil {
		return err
	}

	text.WriteString(
		"Exact package/file/material associations and recipient-only exclusions are recorded in manifest.json. " +
			"Material identifiers below refer to that manifest; complete included text is pooled by SHA-256.\n\n",
	)

	for _, group := range groups {
		writeSourceText(text, group.source)

		err = writePackageTermGroups(text, group.packages)
		if err != nil {
			return err
		}

		err = writeMaterialGroups(text, group.packages)
		if err != nil {
			return err
		}

		text.WriteString("\n")
	}

	return nil
}

func groupTextComponents(records []component) ([]textComponentGroup, error) {
	bySource := map[string]textComponentGroup{}

	for _, record := range records {
		source := sourceIdentity(record)

		reference, err := factReference("s", source)
		if err != nil {
			return nil, err
		}

		group, exists := bySource[reference]
		if exists && !sameFact(group.source, source) {
			return nil, ErrIncomplete
		}

		group.source = source
		group.packages = append(group.packages, record)
		bySource[reference] = group
	}

	groups := make([]textComponentGroup, 0, len(bySource))
	for _, group := range bySource {
		groups = append(groups, group)
	}

	slices.SortFunc(groups, func(left, right textComponentGroup) int {
		if order := strings.Compare(left.source.Module, right.source.Module); order != 0 {
			return order
		}

		return strings.Compare(left.source.URL, right.source.URL)
	})

	return groups, nil
}

func writeSourceText(text *bytes.Buffer, source sourceFact) {
	fmt.Fprintf(text, "Component: %s@%s\nSource: %s\n", source.Module, source.Version, source.URL)

	if source.Replacement == nil {
		if source.Checksum != "" {
			fmt.Fprintf(text, "Selected Go source checksum: %s\n", source.Checksum)
		}

		return
	}

	if source.Replacement.Kind == repositoryReplacementKind {
		fmt.Fprintf(
			text,
			"Repository source content identity: %s\n",
			source.Replacement.ContentHash,
		)

		return
	}

	fmt.Fprintf(
		text,
		"Selected replacement: %s@%s\nSelected Go source checksum: %s\n",
		source.Replacement.Module,
		source.Replacement.Version,
		source.Replacement.Checksum,
	)
}

func writePackageTermGroups(text *bytes.Buffer, records []component) error {
	type terms struct{ Raw, Effective []string }

	groups := map[string][]component{}

	for _, record := range records {
		key, err := factReference("t", terms{record.Raw, record.Effective})
		if err != nil {
			return err
		}

		group := groups[key]
		if len(group) != 0 &&
			(!slices.Equal(group[0].Raw, record.Raw) || !slices.Equal(group[0].Effective, record.Effective)) {
			return ErrIncomplete
		}

		groups[key] = append(group, record)
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	for _, key := range keys {
		group := groups[key]
		first := group[0]
		fmt.Fprintf(
			text,
			"Raw classifier detections: %s\nEffective package-admission terms: %s\n",
			strings.Join(first.Raw, ", "),
			strings.Join(first.Effective, ", "),
		)
		writeScopedTermInstructions(text, first.Effective)
		text.WriteString("Packages:\n")

		for _, record := range group {
			fmt.Fprintln(text, record.Package)
		}
	}

	return nil
}

func writeScopedTermInstructions(text *bytes.Buffer, effective []string) {
	if slices.Contains(effective, "LicenseRef-Loft-KSail-Binary-Permission") {
		text.WriteString(
			"The identified admin-apis component has a maintainer-verified permission record for compilation " +
				"into KSail binaries. The upstream license stays unchanged; no general source grant, " +
				"later-version permission, duration, irrevocability or downstream rights are inferred.\n",
		)
	}

	if slices.Contains(effective, "MPL-2.0") {
		text.WriteString(
			"To obtain the exact MPL covered source, retrieve the source locator above and verify its recorded checksum. " +
				"Repository-source modifications are identified by the release source binding.\n",
		)
	}
}

type textMaterialGroup struct {
	fact  materialFact
	paths map[string]bool
}

func writeMaterialGroups(text *bytes.Buffer, records []component) error {
	groups := map[string]textMaterialGroup{}

	for _, record := range records {
		for _, entry := range record.Materials {
			fact := materialIdentity(entry)

			reference, err := factReference("m", fact)
			if err != nil {
				return err
			}

			group, exists := groups[reference]
			if exists && !sameFact(group.fact, fact) {
				return ErrIncomplete
			}

			if !exists {
				group = textMaterialGroup{fact: fact, paths: map[string]bool{}}
			}

			group.paths[entry.Path] = true
			groups[reference] = group
		}
	}

	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	for _, id := range keys {
		writeMaterialText(text, id, groups[id])
	}

	return nil
}

func writeMaterialText(text *bytes.Buffer, id string, group textMaterialGroup) {
	entry := group.fact
	fmt.Fprintf(text, "Material: %s (%s), SHA-256 %s", id, entry.Kind, entry.SHA256)

	if entry.Disposition != "" {
		fmt.Fprintf(text, "; recipient disposition: %s", entry.Disposition)
	}

	if len(entry.Effective) != 0 {
		fmt.Fprintf(text, "; material terms: %s", strings.Join(entry.Effective, ", "))
	}

	text.WriteString("\n")

	if strings.HasSuffix(entry.Kind, copyrightHeaderKind) {
		return
	}

	paths := make([]string, 0, len(group.paths))
	for path := range group.paths {
		paths = append(paths, path)
	}

	slices.Sort(paths)
	fmt.Fprintf(text, "Files: %s\n", strings.Join(paths, ", "))
}
