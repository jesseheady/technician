package config

import "testing"

func sampleChecks() []CheckConfig {
	return []CheckConfig{
		{Name: "web", Type: CheckTypeHTTP, Group: "Website", Tags: []string{"critical", "public"}},
		{Name: "api", Type: CheckTypeHTTP, Group: "API", Tags: []string{"critical"}},
		{Name: "dns", Type: CheckTypeDNS, Group: "Infrastructure", Tags: []string{"internal"}},
		{Name: "ping", Type: CheckTypeICMP, Group: "Infrastructure"},
		{Name: "flow", Type: CheckTypePlaywright, Group: "Website", Tags: []string{"browser"}},
	}
}

func names(checks []CheckConfig) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name
	}
	return out
}

func equalNames(got []CheckConfig, want ...string) bool {
	g := names(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

func TestFilterChecks(t *testing.T) {
	tests := []struct {
		name   string
		filter CheckFilter
		want   []string
	}{
		{"empty filter runs everything", CheckFilter{}, []string{"web", "api", "dns", "ping", "flow"}},
		{"type OR within dimension", CheckFilter{Types: []string{"http", "dns"}}, []string{"web", "api", "dns"}},
		{"type is case-insensitive", CheckFilter{Types: []string{"HTTP"}}, []string{"web", "api"}},
		{"group filter", CheckFilter{Groups: []string{"Infrastructure"}}, []string{"dns", "ping"}},
		{"group is case-insensitive", CheckFilter{Groups: []string{"website"}}, []string{"web", "flow"}},
		{"tag matches any", CheckFilter{Tags: []string{"critical"}}, []string{"web", "api"}},
		{"tag browser", CheckFilter{Tags: []string{"browser"}}, []string{"flow"}},
		{"dimensions are AND-ed", CheckFilter{Types: []string{"http"}, Groups: []string{"Website"}}, []string{"web"}},
		{"type + tag AND", CheckFilter{Types: []string{"http"}, Tags: []string{"public"}}, []string{"web"}},
		{"no match", CheckFilter{Groups: []string{"Nonexistent"}}, []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterChecks(sampleChecks(), tt.filter)
			if !equalNames(got, tt.want...) {
				t.Errorf("filter %+v: got %v, want %v", tt.filter, names(got), tt.want)
			}
		})
	}
}

func TestConvertCheckPreservesTags(t *testing.T) {
	c, err := convertCheck(checkYAML{
		Name:  "web",
		Type:  CheckTypeHTTP,
		Group: "Website",
		Tags:  []string{"critical", "public"},
		URL:   "https://example.com",
	}, "test.yml")
	if err != nil {
		t.Fatalf("convertCheck: %v", err)
	}
	if len(c.Tags) != 2 || c.Tags[0] != "critical" || c.Tags[1] != "public" {
		t.Errorf("tags not preserved: got %v", c.Tags)
	}
}

func TestCheckFilterValidate(t *testing.T) {
	if err := (CheckFilter{Types: []string{"http", "DNS", "playwright"}}).Validate(); err != nil {
		t.Errorf("known types should validate: %v", err)
	}
	if err := (CheckFilter{Types: []string{"htpp"}}).Validate(); err == nil {
		t.Error("expected error for unknown type 'htpp'")
	}
	// Groups and tags are free-form and never rejected.
	if err := (CheckFilter{Groups: []string{"anything"}, Tags: []string{"whatever"}}).Validate(); err != nil {
		t.Errorf("groups/tags should not be validated: %v", err)
	}
}

func TestCheckFilterIsEmpty(t *testing.T) {
	if !(CheckFilter{}).IsEmpty() {
		t.Error("zero filter should be empty")
	}
	if (CheckFilter{Tags: []string{"x"}}).IsEmpty() {
		t.Error("filter with a tag is not empty")
	}
}

func TestSplitCSV(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"  ", nil},
		{"http", []string{"http"}},
		{"http, dns , tcp", []string{"http", "dns", "tcp"}},
		{"http,,dns,", []string{"http", "dns"}},
	}
	for _, tt := range tests {
		got := SplitCSV(tt.in)
		if len(got) != len(tt.want) {
			t.Errorf("SplitCSV(%q) = %v, want %v", tt.in, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("SplitCSV(%q) = %v, want %v", tt.in, got, tt.want)
				break
			}
		}
	}
}
