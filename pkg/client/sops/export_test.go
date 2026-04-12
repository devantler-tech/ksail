package sops

//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var (
	IsSupportedExtension = isSupportedExtension
	IsHiddenDir          = isHiddenDir
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	ModifyKeyGroups = modifyKeyGroups
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
	RemoveKeyFromGroups = removeKeyFromGroups

//nolint:gochecknoglobals // export_test.go pattern exposes internal helpers as globals.
)
