//go:build !unix

package eksinventory

import "io/fs"

// Other platforms need an equivalent owner/ACL check before enabling capture.
func privateBaselineParent(_ fs.FileInfo) bool { return false }

func privateEvidenceFile(_ fs.FileInfo) bool { return false }
