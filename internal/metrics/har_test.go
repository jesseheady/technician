package metrics

import (
	"testing"
)

func TestParseHARFile(t *testing.T) {
	harJSON := `{
  "log": {
    "entries": [
      {
        "request": {"method": "GET", "url": "https://example.com/"},
        "response": {
          "status": 200,
          "content": {"size": 1024, "mimeType": "text/html"},
          "bodySize": 800,
          "headersSize": 200
        },
        "timings": {"blocked": 0, "dns": 10, "connect": 20, "send": 1, "wait": 50, "receive": 5, "ssl": 15},
        "time": 101
      },
      {
        "request": {"method": "GET", "url": "https://example.com/app.js"},
        "response": {
          "status": 200,
          "content": {"size": 50000, "mimeType": "application/javascript"},
          "bodySize": 30000,
          "headersSize": 100
        },
        "timings": {"blocked": 0, "dns": 0, "connect": 0, "send": 1, "wait": 20, "receive": 10, "ssl": 0},
        "time": 31
      }
    ]
  }
}`

	data, err := ParseHARFile([]byte(harJSON))
	if err != nil {
		t.Fatal(err)
	}

	if len(data.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(data.Entries))
	}

	if data.Entries[0].ResourceType != "document" {
		t.Errorf("expected document, got %s", data.Entries[0].ResourceType)
	}
	if data.Entries[1].ResourceType != "script" {
		t.Errorf("expected script, got %s", data.Entries[1].ResourceType)
	}

	if data.TotalTransferBytes != 30800 {
		t.Errorf("expected total transfer 30800, got %d", data.TotalTransferBytes)
	}
}

func TestClassifyMimeType(t *testing.T) {
	tests := []struct {
		mime     string
		expected string
	}{
		{"text/html", "document"},
		{"application/javascript", "script"},
		{"text/css", "stylesheet"},
		{"image/png", "image"},
		{"image/svg+xml", "image"},
		{"font/woff2", "font"},
		{"application/json", "xhr"},
		{"application/xml", "xhr"},
		{"video/mp4", "other"},
		{"", "other"},
	}

	for _, tt := range tests {
		got := classifyMimeType(tt.mime)
		if got != tt.expected {
			t.Errorf("classifyMimeType(%q) = %s, want %s", tt.mime, got, tt.expected)
		}
	}
}
