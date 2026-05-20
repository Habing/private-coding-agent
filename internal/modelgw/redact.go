package modelgw

import (
	"os"
	"strings"
)

// redact replaces any occurrence of env var values (named in envNames) with
// "[REDACTED]" in s. Only env values >= 8 characters are replaced to avoid
// matching trivially short secrets.
func redact(s string, envNames []string) string {
	for _, name := range envNames {
		v := os.Getenv(name)
		if v != "" && len(v) >= 8 {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return s
}
