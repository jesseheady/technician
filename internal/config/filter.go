package config

import (
	"fmt"
	"sort"
	"strings"
)

// CheckFilter narrows which checks a worker runs, so a single check config
// directory can serve many deployment targets (each with its own
// technician.yml). Each dimension is a set of allowed values: a check must
// match every specified dimension (dimensions are AND-ed) and any value within
// a dimension (values are OR-ed). An empty dimension imposes no restriction, so
// an empty filter runs everything. Matching is case-insensitive.
//
// Filtering happens once at load time — filtered checks are never scheduled,
// not skipped per run.
type CheckFilter struct {
	Types  []string `yaml:"types"`
	Groups []string `yaml:"groups"`
	Tags   []string `yaml:"tags"`
}

// allCheckTypes is the canonical set of check types, used to reject typos in a
// filter's `types` before a worker silently runs nothing.
var allCheckTypes = map[CheckType]bool{
	CheckTypeHTTP: true, CheckTypeSMTP: true, CheckTypeTraceroute: true,
	CheckTypePlaywright: true, CheckTypeTCP: true, CheckTypeDNS: true,
	CheckTypeICMP: true, CheckTypeGRPC: true, CheckTypeNTP: true,
	CheckTypeTLS: true, CheckTypeUDP: true, CheckTypeBGP: true,
	CheckTypeDomainExpiry: true, CheckTypeWebSocket: true,
}

// IsEmpty reports whether the filter restricts nothing.
func (f CheckFilter) IsEmpty() bool {
	return len(f.Types) == 0 && len(f.Groups) == 0 && len(f.Tags) == 0
}

// Validate rejects unknown check types so a misspelled `--types htpp` fails
// loudly instead of matching zero checks. Groups and tags are user-defined and
// cannot be validated.
func (f CheckFilter) Validate() error {
	for _, t := range f.Types {
		if !allCheckTypes[CheckType(strings.ToLower(strings.TrimSpace(t)))] {
			return fmt.Errorf("check_filter: unknown check type %q (valid types: %s)", t, validTypesList())
		}
	}
	return nil
}

// Matches reports whether a check satisfies every specified dimension.
func (f CheckFilter) Matches(c *CheckConfig) bool {
	if len(f.Types) > 0 && !containsFold(f.Types, string(c.Type)) {
		return false
	}
	if len(f.Groups) > 0 && !containsFold(f.Groups, c.Group) {
		return false
	}
	if len(f.Tags) > 0 && !intersectsFold(f.Tags, c.Tags) {
		return false
	}
	return true
}

// FilterChecks returns the checks that match the filter, preserving order. An
// empty filter returns the input unchanged.
func FilterChecks(checks []CheckConfig, f CheckFilter) []CheckConfig {
	if f.IsEmpty() {
		return checks
	}
	out := make([]CheckConfig, 0, len(checks))
	for i := range checks {
		if f.Matches(&checks[i]) {
			out = append(out, checks[i])
		}
	}
	return out
}

// SplitCSV parses a comma-separated flag value into a trimmed, non-empty slice.
func SplitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// containsFold reports whether want case-insensitively equals any of set.
func containsFold(set []string, want string) bool {
	for _, s := range set {
		if strings.EqualFold(strings.TrimSpace(s), want) {
			return true
		}
	}
	return false
}

// intersectsFold reports whether any value in a appears (case-insensitively) in b.
func intersectsFold(a, b []string) bool {
	for _, x := range a {
		if containsFold(b, strings.TrimSpace(x)) {
			return true
		}
	}
	return false
}

func validTypesList() string {
	types := make([]string, 0, len(allCheckTypes))
	for t := range allCheckTypes {
		types = append(types, string(t))
	}
	sort.Strings(types)
	return strings.Join(types, ", ")
}
