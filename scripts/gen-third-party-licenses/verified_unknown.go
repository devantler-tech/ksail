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
// Sibling packages of one repository share a single verification via
// verifyAll — the verification was performed once against that repository.
func verifiedUnknown() map[string]verification {
	out := map[string]verification{
		"github.com/alibabacloud-go/cr-20160607/client": {license: apache2, url: urlAlibabaCR},
		modJSONCanonicalizer: {
			license: apache2,
			url:     "https://github.com/cyberphone/json-canonicalization",
		},
		"github.com/inconshreveable/go-update": {
			license: apache2, url: "https://github.com/inconshreveable/go-update",
		},
		"github.com/loft-sh/admin-apis/pkg/licenseapi": {
			license: apache2, url: "https://github.com/loft-sh/admin-apis",
		},
		modExternalTypes: {
			license: "None published — risk-accepted; upstream license request filed at " +
				"https://github.com/loft-sh/vcluster/issues/4039",
			url: "https://github.com/loft-sh/external-types",
		},
	}

	verifyAll(out, verification{license: apache2, url: urlDeitchMagic},
		"github.com/deitch/magic/pkg/magic",
		"github.com/deitch/magic/pkg/magic/internal",
		"github.com/deitch/magic/pkg/magic/parser",
	)
	verifyAll(out, verification{license: apache2, url: urlAttestation},
		"github.com/in-toto/attestation/go/predicates/provenance/v02",
		"github.com/in-toto/attestation/go/predicates/provenance/v1",
		"github.com/in-toto/attestation/go/v1",
	)
	verifyAll(out, verification{license: apache2, url: urlInToto},
		"github.com/in-toto/in-toto-golang/in_toto",
		"github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/common",
		"github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.1",
		"github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v0.2",
		"github.com/in-toto/in-toto-golang/in_toto/slsa_provenance/v1",
	)
	verifyAll(out, verification{license: mit, url: urlSegmentio},
		"github.com/segmentio/asm/ascii",
		"github.com/segmentio/asm/base64",
		"github.com/segmentio/asm/cpu",
		"github.com/segmentio/asm/cpu/arm",
		"github.com/segmentio/asm/cpu/arm64",
		"github.com/segmentio/asm/cpu/cpuid",
		"github.com/segmentio/asm/cpu/x86",
		"github.com/segmentio/asm/internal/unsafebytes",
		"github.com/segmentio/asm/keyset",
	)

	return out
}

// verifyAll records one repository-level verification for every listed
// package import path.
func verifyAll(dst map[string]verification, entry verification, pkgs ...string) {
	for _, pkg := range pkgs {
		dst[pkg] = entry
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
