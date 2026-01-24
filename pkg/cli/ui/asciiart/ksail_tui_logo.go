package asciiart

// Logo dimensions.
const (
	logoLineCount = 6 // Lines in the block letter logo
)

// Logo returns the KSail ASCII art block letter logo.
// This is the standard logo used across CLI and TUI.
func Logo() string {
	return `██╗  ██╗███████╗ █████╗ ██╗██╗
██║ ██╔╝██╔════╝██╔══██╗██║██║
█████╔╝ ███████╗███████║██║██║
██╔═██╗ ╚════██║██╔══██║██║██║
██║  ██╗███████║██║  ██║██║███████╗
╚═╝  ╚═╝╚══════╝╚═╝  ╚═╝╚═╝╚══════╝`
}

// LogoLines returns the number of lines in the logo.
func LogoLines() int {
	return logoLineCount
}

// CompactLogo returns a minimal single-line logo for space-constrained contexts.
func CompactLogo() string {
	return "⚓ KSail"
}

// Tagline returns the standard KSail tagline.
func Tagline() string {
	return "AI-Powered Kubernetes Assistant"
}
