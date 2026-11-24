package gonotices

import (
	"slices"
	"strings"

	"golang.org/x/mod/module"
)

// upstreamLicenseKind marks reviewed license text that a selected module names
// in its sources but does not ship (for example a fork of a differently
// licensed upstream). Its material path is the upstream Go module locator
// <module>@<version>/<file> the text was taken from.
const (
	upstreamLicenseKind      = "upstream-license"
	upstreamLicenseDirectory = ".github/go-notice-supplements/"
)

// UpstreamLicense is a reviewed, repository-owned supplement: the full text of
// a license an exact selected module names but does not ship. The text lives at
// .github/go-notice-supplements/<Upstream> and must match SHA256. It is
// recipient material only and never an input to package admission.
type UpstreamLicense struct {
	Module   string   `json:"module"`
	Version  string   `json:"version"`
	Sum      string   `json:"sum"`
	Upstream string   `json:"upstream"`
	SHA256   string   `json:"sha256"`
	Terms    []string `json:"terms"`
	Reason   string   `json:"reason"`
}

// UpstreamText is a verified supplement attached to one selected package.
type UpstreamText struct {
	Path  string
	Text  []byte
	Terms []string
}

func validUpstreamLicenses(root string, supplements []UpstreamLicense) bool {
	seen := map[string]bool{}

	for _, supplement := range supplements {
		key := supplement.Module + "\x00" + supplement.Version + "\x00" + supplement.Upstream
		if seen[key] || !validUpstreamLicense(supplement) {
			return false
		}

		seen[key] = true

		_, err := upstreamLicenseText(root, supplement)
		if err != nil {
			return false
		}
	}

	return true
}

func validUpstreamLicense(supplement UpstreamLicense) bool {
	return module.Check(supplement.Module, supplement.Version) == nil &&
		strings.HasPrefix(supplement.Sum, "h1:") &&
		validUpstreamLocator(supplement.Upstream) &&
		sha256Hex(supplement.SHA256) &&
		len(supplement.Terms) != 0 && !slices.Contains(supplement.Terms, "Unknown") &&
		supplement.Reason != ""
}

// validUpstreamLocator accepts only <module>@<version>/<file> naming one file
// of one exact, published upstream module version.
func validUpstreamLocator(locator string) bool {
	path, rest, found := strings.Cut(locator, "@")
	if !found {
		return false
	}

	version, file, found := strings.Cut(rest, "/")

	return found && module.Check(path, version) == nil && materialPath(file) &&
		materialPath(locator)
}

func upstreamLicenseText(root string, supplement UpstreamLicense) ([]byte, error) {
	data, err := readExactMaterial(root, upstreamLicenseDirectory+supplement.Upstream)
	if err != nil || digest(data) != supplement.SHA256 || !publicMaterial(data) {
		return nil, ErrIncomplete
	}

	return data, nil
}

// UpstreamTexts returns the reviewed supplements for one selected package. A
// package of a supplemented module that is replaced, or selected at any other
// exact version or checksum, fails closed rather than losing or misattributing
// the text.
func (policy MaterialPolicy) UpstreamTexts(
	root string,
	pkg Package,
	allowed []string,
) ([]UpstreamText, error) {
	if policy.Schema != 1 {
		return nil, ErrIncomplete
	}

	var texts []UpstreamText

	named := false

	for _, supplement := range policy.Supplements {
		if supplement.Module != pkg.Module.Path {
			continue
		}

		named = true

		if pkg.Module.Replace != nil || supplement.Version != pkg.Module.Version ||
			supplement.Sum != pkg.Module.Sum {
			continue
		}

		text, err := verifiedUpstreamText(root, supplement, allowed)
		if err != nil {
			return nil, err
		}

		texts = append(texts, text)
	}

	if named && len(texts) == 0 {
		return nil, ErrIncomplete
	}

	return texts, nil
}

func verifiedUpstreamText(
	root string,
	supplement UpstreamLicense,
	allowed []string,
) (UpstreamText, error) {
	if !validUpstreamLicense(supplement) || !permittedMaterialTerms(supplement.Terms, allowed) {
		return UpstreamText{}, ErrIncomplete
	}

	data, err := upstreamLicenseText(root, supplement)
	if err != nil {
		return UpstreamText{}, err
	}

	return UpstreamText{
		Path:  supplement.Upstream,
		Text:  data,
		Terms: slices.Clone(supplement.Terms),
	}, nil
}

// VerifySupplementsUsed refuses a reviewed supplement that no generated target
// emitted, so a supplement cannot outlive the exact module version it reviews.
func (policy MaterialPolicy) VerifySupplementsUsed(payloads map[string]Payload) error {
	emitted := map[string]bool{}

	for _, payload := range payloads {
		metadata, err := decodeManifest(payload.Manifest)
		if err != nil {
			return err
		}

		for _, record := range metadata.Components {
			for _, entry := range record.Materials {
				if entry.Kind == upstreamLicenseKind {
					emitted[upstreamLicenseKey(record.Module, record.Version, record.Checksum, entry.Path, entry.SHA256)] = true
				}
			}
		}
	}

	for _, supplement := range policy.Supplements {
		key := upstreamLicenseKey(
			supplement.Module,
			supplement.Version,
			supplement.Sum,
			supplement.Upstream,
			supplement.SHA256,
		)
		if !emitted[key] {
			return ErrIncomplete
		}
	}

	return nil
}

func upstreamLicenseKey(parts ...string) string { return strings.Join(parts, "\x00") }

func upstreamLicenseMaterials(
	source string,
	texts []UpstreamText,
	result *component,
	contents map[string][]byte,
) error {
	for _, text := range texts {
		if !validUpstreamLocator(text.Path) || len(text.Text) == 0 || !publicMaterial(text.Text) ||
			len(text.Terms) == 0 {
			return ErrIncomplete
		}

		sum := digest(text.Text)
		result.Materials = append(result.Materials, material{
			Source:      source,
			Path:        text.Path,
			Kind:        upstreamLicenseKind,
			SHA256:      sum,
			Effective:   slices.Clone(text.Terms),
			Disposition: licenseDisposition,
		})
		contents[sum] = text.Text
	}

	slices.SortFunc(result.Materials, func(left, right material) int {
		return strings.Compare(left.Path, right.Path)
	})

	for index := 1; index < len(result.Materials); index++ {
		if result.Materials[index-1].Path == result.Materials[index].Path {
			return ErrIncomplete
		}
	}

	return nil
}
