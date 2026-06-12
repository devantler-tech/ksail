package sops

//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var (
	IsSupportedExtension         = isSupportedExtension
	IsHiddenDir                  = isHiddenDir
	ModifyKeyGroups              = modifyKeyGroups
	RemoveKeyFromGroups          = removeKeyFromGroups
	HashFile                     = hashFile
	LookupAnyEditor              = lookupAnyEditor
	ParseEditorCommand           = parseEditorCommand
	HandleEmitError              = handleEmitError
	FormatAgeKeyWithMetadata     = formatAgeKeyWithMetadata
	ValidateAgeKey               = validateAgeKey
	MetadataFromEncryptionConfig = metadataFromEncryptionConfig
)
