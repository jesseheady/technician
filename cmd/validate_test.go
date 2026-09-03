package cmd

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestRunValidateHonorsRetry proves that `technician validate` applies a check's
// retry policy, the same way the scheduler does. Without it, one transient
// failure from a third party fails the whole run. See issue #363.
func TestRunValidateHonorsRetry(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError) // one transient failure
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	cfgPath := write("technician.yml", "service: test\nhostname: test\norigins:\n  - id: local\n    city: Local\n    country: US\n")
	write("checks.yml", fmt.Sprintf(`- name: retry-target
  type: http
  url: %s
  timeout: 5s
  retry:
    count: 1
    delay: 10ms
`, srv.URL))
	budgetsPath := write("budgets.yml", "[]\n")

	// runValidate reads package-level flag state; restore it for other tests.
	defer func(c, b, o string, fe, fb bool) {
		cfgFile, budgetFile, validateOutput, failOnError, failOnBudget = c, b, o, fe, fb
	}(cfgFile, budgetFile, validateOutput, failOnError, failOnBudget)
	cfgFile, budgetFile, validateOutput = cfgPath, budgetsPath, "text"
	// Both are exit paths; runValidate would call os.Exit and kill the test run.
	failOnError, failOnBudget = false, false

	if err := runValidate(nil, nil); err != nil {
		t.Fatalf("runValidate: %v", err)
	}

	if got := requests.Load(); got != 2 {
		t.Errorf("expected 2 requests (initial attempt + 1 retry), got %d", got)
	}
}
