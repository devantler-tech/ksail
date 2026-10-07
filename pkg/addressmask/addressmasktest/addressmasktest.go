// Package addressmasktest helps tests assert that text names no server address.
package addressmasktest

import (
	"net/netip"
	"strings"
)

// PublicAddressesIn finds publicly routable addresses in text without using the
// masker's own pattern, so a gap in that pattern cannot hide from a test.
func PublicAddressesIn(text string) []string {
	var found []string

	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !strings.ContainsRune("0123456789abcdefABCDEF:.", r)
	})

	for _, field := range fields {
		trimmed := strings.TrimRight(field, ":.")

		for _, candidate := range []string{field, trimmed, portless(trimmed)} {
			addr, err := netip.ParseAddr(candidate)
			if err != nil {
				continue
			}

			addr = addr.Unmap()
			if addr.IsGlobalUnicast() && !addr.IsPrivate() {
				found = append(found, candidate)

				break
			}
		}
	}

	return found
}

// portless drops a trailing ":port" from a dotted-quad field.
func portless(field string) string {
	if strings.Count(field, ":") != 1 {
		return field
	}

	host, _, _ := strings.Cut(field, ":")

	return host
}
