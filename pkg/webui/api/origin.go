package api

import (
	"net"
	"net/url"
	"strings"
)

// desktopOrigin is the one origin the desktop shell serves the SPA from, through the Wails AssetServer
// with no TCP listener at all (see desktop/main.go).
const desktopOrigin = "wails://wails"

// isTrustedLocalOrigin reports whether a browser-sent Origin header names a page served from this
// machine: the desktop shell's fixed origin, or a page on the loopback interface. Every endpoint that
// trusts a local page applies this one rule, so a change to what counts as local reaches all of them.
//
// The trusted origin is never derived from the request. Host is client-controlled, so under DNS
// rebinding an attacker controls both it and a matching Origin; a browser sets Origin from the page's
// real URL, so only a page actually served from loopback can present a loopback origin.
//
// The desktop origin is compared as the raw header, not as parsed fields. A browser-sent Origin is a
// scheme and an authority and nothing else, and an empty delimiter parses away to nothing:
// "wails://wails?" and "wails://wails#" both leave RawQuery and Fragment empty. Every other value under
// the wails scheme (another authority, a port, user info, a path, a different letter case) is
// rejected rather than falling through to the loopback test, so "wails://localhost" is not trusted.
func isTrustedLocalOrigin(origin string) bool {
	if origin == desktopOrigin {
		return true
	}

	parsed, err := url.Parse(origin)
	if err != nil || strings.EqualFold(parsed.Scheme, "wails") {
		return false
	}

	return isLoopbackHost(parsed.Hostname())
}

// isLoopbackHost reports whether a URL hostname names this machine's loopback interface. "localhost" is
// included: an attacker can rebind their own name to 127.0.0.1, but cannot make a browser report
// "localhost" as the origin of a page they serve.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}

	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}
