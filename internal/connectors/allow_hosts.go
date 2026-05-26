package connectors

import (
	"fmt"
	"strings"
)

const maxAllowHosts = 128

// NormalizeAllowHosts trims, lowercases, deduplicates, and drops empty host patterns.
func NormalizeAllowHosts(hosts []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			continue
		}
		if _, ok := seen[h]; ok {
			continue
		}
		if err := validateAllowHostPattern(h); err != nil {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out
}

// ValidateAllowHosts rejects empty lists and invalid patterns.
func ValidateAllowHosts(hosts []string) ([]string, error) {
	if len(hosts) == 0 {
		return nil, fmt.Errorf("allow_hosts must not be empty")
	}
	if len(hosts) > maxAllowHosts {
		return nil, fmt.Errorf("allow_hosts exceeds maximum of %d entries", maxAllowHosts)
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(hosts))
	for _, h := range hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if h == "" {
			return nil, fmt.Errorf("allow_hosts contains empty entry")
		}
		if err := validateAllowHostPattern(h); err != nil {
			return nil, err
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		out = append(out, h)
	}
	return out, nil
}

func validateAllowHostPattern(h string) error {
	if h == "*" {
		return nil
	}
	if strings.HasPrefix(h, "*.") {
		suffix := strings.TrimPrefix(h, "*.")
		if suffix == "" || strings.Contains(suffix, "*") {
			return fmt.Errorf("invalid wildcard host %q", h)
		}
		return nil
	}
	if strings.ContainsAny(h, " \t/:*") {
		return fmt.Errorf("invalid host %q", h)
	}
	return nil
}
