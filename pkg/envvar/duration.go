package envvar

import (
	"log/slog"
	"os"
	"time"
)

// Duration returns the time.Duration parsed from the named environment variable.
// It falls back to fallback when the variable is unset or empty, and logs a
// warning then falls back when the value is set but unparseable or non-positive.
// Values use Go duration syntax (e.g. "15m", "1200s", "1h30m").
func Duration(name string, fallback time.Duration) time.Duration {
	raw, ok := os.LookupEnv(name)
	if !ok || raw == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(raw)
	if err != nil {
		slog.Warn(
			"ignoring invalid duration in environment variable; using default",
			"name", name,
			"value", raw,
			"default", fallback,
			"error", err,
		)

		return fallback
	}

	if parsed <= 0 {
		slog.Warn(
			"ignoring non-positive duration in environment variable; using default",
			"name", name,
			"value", raw,
			"default", fallback,
		)

		return fallback
	}

	return parsed
}
