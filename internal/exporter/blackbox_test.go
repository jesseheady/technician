package exporter

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBlackboxHandlerMissingTarget(t *testing.T) {
	handler := NewBlackboxHandler()
	req := httptest.NewRequest("GET", "/probe", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestBlackboxHandlerUnknownModule(t *testing.T) {
	handler := NewBlackboxHandler()
	req := httptest.NewRequest("GET", "/probe?target=http://example.com&module=unknown", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestBlackboxHandlerHTTP(t *testing.T) {
	// Set up a test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	handler := NewBlackboxHandler()
	req := httptest.NewRequest("GET", "/probe?target="+ts.URL+"&module=http_2xx", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if len(body) == 0 {
		t.Error("expected non-empty metrics response")
	}
}

func TestBuildCheckConfigHTTP(t *testing.T) {
	cfg := buildCheckConfig("https://example.com", "http_2xx")
	if cfg.HTTP == nil {
		t.Fatal("expected HTTP config")
	}
	if cfg.HTTP.URL != "https://example.com" {
		t.Errorf("expected URL=https://example.com, got %s", cfg.HTTP.URL)
	}
}

func TestBuildCheckConfigSMTP(t *testing.T) {
	cfg := buildCheckConfig("mail.example.com", "smtp")
	if cfg.SMTP == nil {
		t.Fatal("expected SMTP config")
	}
	if cfg.SMTP.Host != "mail.example.com" {
		t.Errorf("expected host=mail.example.com, got %s", cfg.SMTP.Host)
	}
}
