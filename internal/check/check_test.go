package check

import "testing"

func TestResultThresholdDefaults(t *testing.T) {
	// Zero values fall back to the documented defaults.
	r := &Result{}
	if got := r.CertWarnDays(); got != 30 {
		t.Errorf("CertWarnDays default = %d, want 30", got)
	}
	if got := r.CertCriticalDays(); got != 7 {
		t.Errorf("CertCriticalDays default = %d, want 7", got)
	}
	if got := r.DomainWarnDays(); got != 30 {
		t.Errorf("DomainWarnDays default = %d, want 30", got)
	}
	if got := r.DomainCriticalDays(); got != 7 {
		t.Errorf("DomainCriticalDays default = %d, want 7", got)
	}
}

func TestResultThresholdOverrides(t *testing.T) {
	// Positive values are returned as-is.
	r := &Result{
		CertWarnDaysVal:   45,
		CertCritDaysVal:   10,
		DomainWarnDaysVal: 60,
		DomainCritDaysVal: 14,
	}
	if got := r.CertWarnDays(); got != 45 {
		t.Errorf("CertWarnDays = %d, want 45", got)
	}
	if got := r.CertCriticalDays(); got != 10 {
		t.Errorf("CertCriticalDays = %d, want 10", got)
	}
	if got := r.DomainWarnDays(); got != 60 {
		t.Errorf("DomainWarnDays = %d, want 60", got)
	}
	if got := r.DomainCriticalDays(); got != 14 {
		t.Errorf("DomainCriticalDays = %d, want 14", got)
	}
}
