package main

const (
	apache2 = "Apache-2.0"
	mit     = "MIT"

	urlAlibabaCR   = "https://github.com/alibabacloud-go/cr-20160607"
	urlDeitchMagic = "https://github.com/deitch/magic"
	urlAttestation = "https://github.com/in-toto/attestation"
	urlInToto      = "https://github.com/in-toto/in-toto-golang"
	urlSegmentio   = "https://github.com/segmentio/asm"

	modJSONCanonicalizer = "github.com/cyberphone/json-canonicalization" +
		"/go/src/webpki.org/jsoncanonicalizer"
	modExternalTypes = "github.com/loft-sh/external-types" +
		"/argoproj/argo-cd/v2/pkg/apis/application/v1alpha1"
)

// verification records a manual license verification for a module whose Go
// module archive bundles no license file (go-licenses reports it "Unknown").
// Each entry MUST be verified against the module's source repository before
// being added; the generator fails on any Unknown module missing here.
type verification struct {
	// license is the verified SPDX identifier, or a short explanation when the
	// project publishes no license at all.
	license string
	// url is the source repository the verification was performed against.
	url string
}

// verifiedUnknown maps go-licenses "Unknown" modules to their manually
// verified licenses. Most entries are false negatives: the repository ships a
// LICENSE at its root that go-licenses cannot discover at the sub-package
// level (the same set CI's `go-licenses check --ignore` flags document).
func verifiedUnknown() map[string]verification {
	return map[string]verification{
		"github.com/alibabacloud-go/cr-20160607/client": {apache2, urlAlibabaCR},
		modJSONCanonicalizer: {
			apache2,
			"https://github.com/cyberphone/json-canonicalization",
		},
		"github.com/deitch/magic/pkg/magic": {
			apache2,
			urlDeitchMagic,
		},
		"github.com/deitch/magic/pkg/magic/internal": {
			apache2,
			urlDeitchMagic,
		},
		"github.com/deitch/magic/pkg/magic/parser": {
			apache2,
			urlDeitchMagic,
		},
		"github.com/in-toto/attestation/go/predicates/provenance/v02": {
			apache2,
			urlAttestation,
		},
		"github.com/in-toto/attestation/go/predicates/provenance/v1": {
			apache2,
			urlAttestation,
		},
		"github.com/in-toto/attestation/go/v1": {
			apache2,
			urlAttestation,
		},
		"github.com/in-toto/in-toto-golang/in_toto":                        {apache2, urlInToto},
		"github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/common": {apache2, urlInToto},
		"github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.1":   {apache2, urlInToto},
		"github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2":   {apache2, urlInToto},
		"github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1":     {apache2, urlInToto},
		"github.com/inconshreveable/go-update": {
			apache2, "https://github.com/inconshreveable/go-update",
		},
		"github.com/loft-sh/admin-apis/pkg/licenseapi": {
			apache2, "https://github.com/loft-sh/admin-apis",
		},
		modExternalTypes: {
			"None published — risk-accepted; upstream license request filed at " +
				"https://github.com/loft-sh/vcluster/issues/4039",
			"https://github.com/loft-sh/external-types",
		},
		"github.com/segmentio/asm/ascii":                {mit, urlSegmentio},
		"github.com/segmentio/asm/base64":               {mit, urlSegmentio},
		"github.com/segmentio/asm/cpu":                  {mit, urlSegmentio},
		"github.com/segmentio/asm/cpu/arm":              {mit, urlSegmentio},
		"github.com/segmentio/asm/cpu/arm64":            {mit, urlSegmentio},
		"github.com/segmentio/asm/cpu/cpuid":            {mit, urlSegmentio},
		"github.com/segmentio/asm/cpu/x86":              {mit, urlSegmentio},
		"github.com/segmentio/asm/internal/unsafebytes": {mit, urlSegmentio},
		"github.com/segmentio/asm/keyset":               {mit, urlSegmentio},
	}
}

// override pins a module whose go-licenses classification is wrong, ambiguous,
// or non-deterministic, recording the manually verified (or elected)
// classification and the reason.
type override struct {
	elected string
	note    string
}

// classificationOverrides pins modules whose go-licenses classification is
// wrong, ambiguous, or non-deterministic: dual-licensed modules (recording
// the license this project elects) and modules the classifier flaps on
// run-to-run (which would break the byte-identical-output guarantee CI's
// drift check relies on).
func classificationOverrides() map[string]override {
	return map[string]override{
		// LICENSE.code: "Apache-2.0 OR GPL-2.0-or-later"; Apache-2.0 elected.
		"github.com/spdx/tools-golang": {
			elected: apache2,
			note: "dual-licensed Apache-2.0 OR GPL-2.0-or-later (LICENSE.code); " +
				"Apache-2.0 elected",
		},
		// LICENSE is the plain MIT text (verified 2026-07-04), but go-licenses
		// classifies it as MIT on one run and Unicode-DFS-2016 on another —
		// pinned so regeneration stays deterministic.
		"github.com/apparentlymart/go-textseg/v15/textseg": {
			elected: mit,
			note: "LICENSE is the MIT text; pinned (go-licenses classification " +
				"alternates MIT/Unicode-DFS-2016)",
		},
	}
}
