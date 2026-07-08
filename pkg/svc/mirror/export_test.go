package mirror

// DefaultSteerImageForVersion exposes defaultSteerImage to the black-box test
// package so the version-pinned steer image derivation can be exercised for
// stamped release builds and unstamped/dev builds alike.
func DefaultSteerImageForVersion(version string) string {
	return defaultSteerImage(version)
}
