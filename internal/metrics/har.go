package metrics

import (
	"encoding/json"
	"fmt"

	"github.com/jesseheady/technician/internal/probe"
)

// HARFile represents the top-level HAR 1.2 structure.
type HARFile struct {
	Log HARLog `json:"log"`
}

type HARLog struct {
	Entries []HARFileEntry `json:"entries"`
}

type HARFileEntry struct {
	Request  HARRequest  `json:"request"`
	Response HARResponse `json:"response"`
	Timings  HARTimings  `json:"timings"`
	Time     float64     `json:"time"`
}

type HARRequest struct {
	Method string `json:"method"`
	URL    string `json:"url"`
}

type HARResponse struct {
	Status  int `json:"status"`
	Content struct {
		Size     int64  `json:"size"`
		MimeType string `json:"mimeType"`
	} `json:"content"`
	BodySize    int64 `json:"bodySize"`
	HeadersSize int64 `json:"headersSize"`
}

type HARTimings struct {
	Blocked float64 `json:"blocked"`
	DNS     float64 `json:"dns"`
	Connect float64 `json:"connect"`
	Send    float64 `json:"send"`
	Wait    float64 `json:"wait"`
	Receive float64 `json:"receive"`
	SSL     float64 `json:"ssl"`
}

// ParseHARFile parses a HAR JSON file into probe.HARData.
func ParseHARFile(data []byte) (*probe.HARData, error) {
	var har HARFile
	if err := json.Unmarshal(data, &har); err != nil {
		return nil, fmt.Errorf("parsing HAR: %w", err)
	}

	result := &probe.HARData{
		Entries: make([]probe.HAREntry, len(har.Log.Entries)),
	}

	var totalTransfer int64
	for i, entry := range har.Log.Entries {
		resourceType := classifyMimeType(entry.Response.Content.MimeType)
		transferSize := entry.Response.BodySize
		if transferSize < 0 {
			transferSize = entry.Response.Content.Size
		}
		totalTransfer += transferSize

		result.Entries[i] = probe.HAREntry{
			URL:          entry.Request.URL,
			ResourceType: resourceType,
			Duration:     entry.Time,
			TransferSize: transferSize,
			ResponseSize: entry.Response.Content.Size,
			Status:       entry.Response.Status,
		}
	}
	result.TotalTransferBytes = totalTransfer

	return result, nil
}

func classifyMimeType(mime string) string {
	switch {
	case mime == "":
		return "other"
	case contains(mime, "html"):
		return "document"
	case contains(mime, "javascript"), contains(mime, "ecmascript"):
		return "script"
	case contains(mime, "css"):
		return "stylesheet"
	case contains(mime, "image"):
		return "image"
	case contains(mime, "font"), contains(mime, "woff"), contains(mime, "ttf"), contains(mime, "otf"):
		return "font"
	case contains(mime, "json"), contains(mime, "xml"):
		return "xhr"
	default:
		return "other"
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
