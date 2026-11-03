package gonotices

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"slices"
	"strings"
)

// MaterialRule is a recipient-only decision about one exact source declaration.
// It is never an input to dependency package admission.
type MaterialRule struct {
	Module          string             `json:"module"`
	Version         string             `json:"version"`
	Sum             string             `json:"sum"`
	Path            string             `json:"path"`
	SHA256          string             `json:"sha256"`
	Packages        []string           `json:"packages"`
	Detected        []string           `json:"detected"`
	Terms           []string           `json:"terms,omitempty"`
	Role            string             `json:"role"`
	Evidence        []MaterialEvidence `json:"evidence"`
	NoEmbeddedFiles bool               `json:"noEmbeddedFiles,omitempty"`
	Reason          string             `json:"reason"`
}

// MaterialEvidence binds primary source statements supporting applicability.
type MaterialEvidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// MaterialPolicy contains closed, reviewed source-material decisions.
type MaterialPolicy struct {
	Schema int            `json:"schema"`
	Files  []MaterialRule `json:"files"`
}

// LoadMaterialPolicy reads only the fixed repository-owned recipient decisions.
func LoadMaterialPolicy(root string) (MaterialPolicy, error) {
	data, err := readMaterial(root, ".github/go-notice-materials.json")
	if err != nil {
		return MaterialPolicy{}, ErrIncomplete
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()

	var policy MaterialPolicy
	if decoder.Decode(&policy) != nil || decoder.Decode(new(any)) != io.EOF || policy.Schema != 1 {
		return MaterialPolicy{}, ErrIncomplete
	}

	canonical, err := json.MarshalIndent(policy, "", "  ")
	if err != nil || !bytes.Equal(append(canonical, '\n'), data) || !publicMaterial(data) ||
		!validMaterialRules(policy.Files) {
		return MaterialPolicy{}, ErrIncomplete
	}

	return policy, nil
}

const (
	licenseDisposition     = "license"
	attributionDisposition = "attribution"
	excludedDisposition    = "excluded"
	sha256ByteCount        = 32
)

func validMaterialRules(rules []MaterialRule) bool {
	seen := map[string]bool{}
	for _, rule := range rules {
		if !validMaterialRule(rule) || !uniqueMaterialPackages(rule, seen) {
			return false
		}
	}

	return true
}

func validMaterialRule(rule MaterialRule) bool {
	return validMaterialRuleIdentity(rule) && validMaterialRuleDescription(rule) &&
		validMaterialRoleShape(rule) &&
		validMaterialEvidence(rule.Evidence)
}

func validMaterialRuleIdentity(rule MaterialRule) bool {
	return rule.Module != "" && rule.Version != "" && strings.HasPrefix(rule.Sum, "h1:") &&
		materialPath(rule.Path) &&
		sha256Hex(rule.SHA256)
}

func validMaterialRuleDescription(rule MaterialRule) bool {
	return len(rule.Packages) != 0 && len(rule.Detected) != 0 && len(rule.Evidence) != 0 &&
		rule.Reason != ""
}

func validMaterialRoleShape(rule MaterialRule) bool {
	switch rule.Role {
	case licenseDisposition:
		return len(rule.Terms) != 0 && !rule.NoEmbeddedFiles
	case attributionDisposition:
		return len(rule.Terms) == 0 && !rule.NoEmbeddedFiles
	case excludedDisposition:
		return len(rule.Terms) == 0 && rule.NoEmbeddedFiles
	default:
		return false
	}
}

func validMaterialEvidence(evidence []MaterialEvidence) bool {
	seen := map[string]bool{}
	for _, entry := range evidence {
		if !materialPath(entry.Path) || !sha256Hex(entry.SHA256) || seen[entry.Path] {
			return false
		}

		seen[entry.Path] = true
	}

	return true
}

func uniqueMaterialPackages(rule MaterialRule, seen map[string]bool) bool {
	for _, pkg := range rule.Packages {
		key := rule.Module + "\x00" + rule.Version + "\x00" + rule.Sum + "\x00" + rule.Path + "\x00" + pkg
		if seen[key] || pkg != rule.Module && !strings.HasPrefix(pkg, rule.Module+"/") {
			return false
		}

		seen[key] = true
	}

	return true
}

func materialPath(path string) bool {
	return filepath.IsLocal(path) && path != "." &&
		filepath.ToSlash(filepath.Clean(path)) == path &&
		!strings.ContainsAny(path, "\\\r\n\x00")
}

func sha256Hex(value string) bool {
	decoded, err := hex.DecodeString(value)

	return err == nil && len(decoded) == sha256ByteCount && strings.ToLower(value) == value
}

// Covers identifies a reviewed candidate independently of its current bytes or
// version. A changed identity must fail Resolve, never fall back to an exclusion
// or generic allowed-term decision.
func (policy MaterialPolicy) Covers(pkg Package, path string) bool {
	for _, rule := range policy.Files {
		if rule.Module == pkg.Module.Path && rule.Path == path &&
			slices.Contains(rule.Packages, pkg.ImportPath) {
			return true
		}
	}

	return false
}

// Resolve refuses every declaration without a complete exact-source match.
func (policy MaterialPolicy) Resolve(
	pkg Package,
	path string,
	detected, allowed []string,
) (Declaration, error) {
	if policy.Schema != 1 || pkg.Module.Main || pkg.Module.Replace != nil || !materialPath(path) {
		return Declaration{}, ErrIncomplete
	}

	matched, err := policy.exactMaterialRule(pkg, path)
	if err != nil {
		return Declaration{}, err
	}

	if !sameNames(matched.Detected, detected) || len(matched.Evidence) == 0 {
		return Declaration{}, ErrIncomplete
	}

	err = verifyMaterialRuleFiles(pkg.Module.Directory, *matched)
	if err != nil {
		return Declaration{}, err
	}

	if !permittedMaterialRole(*matched, pkg, allowed) {
		return Declaration{}, ErrIncomplete
	}

	return Declaration{
		Path:      path,
		Raw:       slices.Clone(detected),
		Effective: slices.Clone(matched.Terms),
		Role:      matched.Role,
	}, nil
}

func (policy MaterialPolicy) exactMaterialRule(pkg Package, path string) (*MaterialRule, error) {
	var matched *MaterialRule

	for _, rule := range policy.Files {
		if !materialRuleMatches(rule, pkg, path) {
			continue
		}

		if matched != nil {
			return nil, ErrIncomplete
		}

		copyOfRule := rule
		matched = &copyOfRule
	}

	if matched == nil {
		return nil, ErrIncomplete
	}

	return matched, nil
}

func materialRuleMatches(rule MaterialRule, pkg Package, path string) bool {
	return rule.Module == pkg.Module.Path && rule.Version == pkg.Module.Version &&
		rule.Sum == pkg.Module.Sum &&
		rule.Path == path &&
		slices.Contains(rule.Packages, pkg.ImportPath)
}

func verifyMaterialRuleFiles(directory string, rule MaterialRule) error {
	data, err := readMaterial(directory, rule.Path)
	if err != nil || digest(data) != rule.SHA256 {
		return ErrIncomplete
	}

	for _, evidence := range rule.Evidence {
		data, err = readMaterial(directory, evidence.Path)
		if err != nil || digest(data) != evidence.SHA256 {
			return ErrIncomplete
		}
	}

	return nil
}

func permittedMaterialRole(rule MaterialRule, pkg Package, allowed []string) bool {
	switch rule.Role {
	case licenseDisposition:
		return permittedMaterialTerms(rule.Terms, allowed)
	case attributionDisposition:
		return len(rule.Terms) == 0
	case excludedDisposition:
		return len(rule.Terms) == 0 && rule.NoEmbeddedFiles && len(pkg.EmbedFiles) == 0
	default:
		return false
	}
}

func permittedMaterialTerms(terms, allowed []string) bool {
	if len(terms) == 0 {
		return false
	}

	for _, term := range terms {
		if term == "Unknown" || !slices.Contains(allowed, term) {
			return false
		}
	}

	return true
}

func sameNames(left, right []string) bool {
	first, second := slices.Clone(left), slices.Clone(right)
	slices.Sort(first)
	slices.Sort(second)

	return len(first) != 0 && slices.Equal(first, second)
}
