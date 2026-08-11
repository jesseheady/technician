package remotewrite

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/snappy"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/protobuf/encoding/protowire"
)

// testRegistry returns a registry with one technician_* gauge and one unrelated
// self-health gauge, so filtering and encoding can both be checked.
func testRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()
	reg := prometheus.NewRegistry()
	g := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "technician_check_healthy", Help: "test"}, []string{"type", "region"})
	other := prometheus.NewGauge(prometheus.GaugeOpts{Name: "go_goroutines", Help: "test"})
	reg.MustRegister(g, other)
	g.WithLabelValues("http", "us-east-1").Set(1)
	other.Set(7)
	return reg
}

func gather(t *testing.T, reg *prometheus.Registry) []*dto.MetricFamily {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	return mfs
}

func TestEncodeWriteRequestRoundTrip(t *testing.T) {
	const ts = int64(1_700_000_000_000)
	payload := encodeWriteRequest(gather(t, testRegistry(t)), ts)
	if payload == nil {
		t.Fatal("expected a payload")
	}
	series := decodeWriteRequest(t, payload)

	if len(series) != 1 {
		t.Fatalf("got %d series, want 1 (go_goroutines must be filtered out)", len(series))
	}
	s := series[0]
	want := map[string]string{"__name__": "technician_check_healthy", "type": "http", "region": "us-east-1"}
	if len(s.labels) != len(want) {
		t.Fatalf("labels = %v, want %v", s.labels, want)
	}
	for k, v := range want {
		if s.labels[k] != v {
			t.Errorf("label %q = %q, want %q", k, s.labels[k], v)
		}
	}
	if s.value != 1 {
		t.Errorf("value = %v, want 1", s.value)
	}
	if s.tsMs != ts {
		t.Errorf("timestamp = %d, want %d", s.tsMs, ts)
	}
}

func TestEncodeWriteRequestEmptyWhenNoTechnicianMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: "process_cpu_seconds_total", Help: "test"})
	reg.MustRegister(g)
	g.Set(3)
	if payload := encodeWriteRequest(gather(t, reg), 1); payload != nil {
		t.Errorf("expected nil payload when no technician_* metrics, got %d bytes", len(payload))
	}
}

func TestPushSendsSnappyProtobuf(t *testing.T) {
	var gotEncoding, gotVersion string
	received := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotEncoding = r.Header.Get("Content-Encoding")
		gotVersion = r.Header.Get("X-Prometheus-Remote-Write-Version")
		body, _ := io.ReadAll(r.Body)
		received <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := &Writer{url: srv.URL, gatherer: testRegistry(t), client: srv.Client()}
	if err := w.push(context.Background()); err != nil {
		t.Fatalf("push: %v", err)
	}

	if gotEncoding != "snappy" {
		t.Errorf("Content-Encoding = %q, want snappy", gotEncoding)
	}
	if gotVersion != "0.1.0" {
		t.Errorf("remote-write version header = %q, want 0.1.0", gotVersion)
	}

	compressed := <-received
	raw, err := snappy.Decode(nil, compressed)
	if err != nil {
		t.Fatalf("snappy decode: %v", err)
	}
	if series := decodeWriteRequest(t, raw); len(series) != 1 || series[0].labels["__name__"] != "technician_check_healthy" {
		t.Fatalf("decoded series = %+v, want one technician_check_healthy", series)
	}
}

func TestNewRejectsEmptyURL(t *testing.T) {
	if _, err := New(context.Background(), Options{}); err == nil {
		t.Error("expected an error for empty URL")
	}
}

func TestNewDefaultsInterval(t *testing.T) {
	w, err := New(context.Background(), Options{URL: "http://localhost:9090/api/v1/write"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if w.interval != 15*time.Second {
		t.Errorf("interval = %v, want 15s default", w.interval)
	}
}

// --- minimal protobuf decoder for the remote-write WriteRequest, test-only ---

type series struct {
	labels map[string]string
	value  float64
	tsMs   int64
}

func decodeWriteRequest(t *testing.T, b []byte) []series {
	t.Helper()
	var out []series
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 {
			t.Fatal("bad tag")
		}
		b = b[n:]
		if num == 1 && typ == protowire.BytesType {
			v, m := protowire.ConsumeBytes(b)
			if m < 0 {
				t.Fatal("bad timeseries")
			}
			b = b[m:]
			out = append(out, decodeTimeSeries(t, v))
			continue
		}
		b = b[protowire.ConsumeFieldValue(num, typ, b):]
	}
	return out
}

func decodeTimeSeries(t *testing.T, b []byte) series {
	t.Helper()
	s := series{labels: map[string]string{}}
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		b = b[n:]
		v, m := protowire.ConsumeBytes(b)
		if m < 0 {
			t.Fatal("bad sub-message")
		}
		b = b[m:]
		switch num {
		case 1:
			k, val := decodeLabel(v)
			s.labels[k] = val
		case 2:
			s.value, s.tsMs = decodeSample(v)
		}
		_ = typ
	}
	return s
}

func decodeLabel(b []byte) (name, value string) {
	for len(b) > 0 {
		num, _, n := protowire.ConsumeTag(b)
		b = b[n:]
		v, m := protowire.ConsumeBytes(b)
		b = b[m:]
		if num == 1 {
			name = string(v)
		} else {
			value = string(v)
		}
	}
	return name, value
}

func decodeSample(b []byte) (value float64, tsMs int64) {
	for len(b) > 0 {
		num, typ, n := protowire.ConsumeTag(b)
		b = b[n:]
		switch num {
		case 1:
			u, m := protowire.ConsumeFixed64(b)
			b = b[m:]
			value = math.Float64frombits(u)
		case 2:
			u, m := protowire.ConsumeVarint(b)
			b = b[m:]
			tsMs = int64(u)
		default:
			b = b[protowire.ConsumeFieldValue(num, typ, b):]
		}
	}
	return value, tsMs
}
