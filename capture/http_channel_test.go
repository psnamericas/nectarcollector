package capture

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nectarcollector/config"
)

// mockDualWriter implements a minimal writer for testing
type mockDualWriter struct {
	lines []string
	err   error
}

func (m *mockDualWriter) WriteLine(line string) error {
	if m.err != nil {
		return m.err
	}
	m.lines = append(m.lines, line)
	return nil
}

func (m *mockDualWriter) Close() error {
	return nil
}

func TestNewHTTPChannel(t *testing.T) {
	portCfg := config.PortConfig{
		Type:            "http",
		Path:            "/test/endpoint",
		SideDesignation: "A1",
		FIPSCode:        "1234567890",
	}
	appCfg := config.AppConfig{
		Name:       "Test",
		InstanceID: "test-01",
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ch := NewHTTPChannel(portCfg, appCfg, nil, logger)

	if ch.Path() != "/test/endpoint" {
		t.Errorf("Path() = %q, want %q", ch.Path(), "/test/endpoint")
	}
	if ch.SideDesignation() != "A1" {
		t.Errorf("SideDesignation() = %q, want %q", ch.SideDesignation(), "A1")
	}
	if ch.Config().FIPSCode != "1234567890" {
		t.Errorf("Config().FIPSCode = %q, want %q", ch.Config().FIPSCode, "1234567890")
	}
}

func TestHTTPChannelStats(t *testing.T) {
	portCfg := config.PortConfig{
		Type:            "http",
		Path:            "/test",
		SideDesignation: "A1",
	}
	appCfg := config.AppConfig{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ch := NewHTTPChannel(portCfg, appCfg, nil, logger)
	stats := ch.GetStats()

	if stats.BytesRead != 0 {
		t.Errorf("BytesRead = %d, want 0", stats.BytesRead)
	}
	if stats.RequestCount != 0 {
		t.Errorf("RequestCount = %d, want 0", stats.RequestCount)
	}
	if stats.Errors != 0 {
		t.Errorf("Errors = %d, want 0", stats.Errors)
	}
	if stats.StartTime.IsZero() {
		t.Error("StartTime should not be zero")
	}
}

func TestHTTPChannelMethodNotAllowed(t *testing.T) {
	portCfg := config.PortConfig{
		Type:            "http",
		Path:            "/test",
		SideDesignation: "A1",
	}
	appCfg := config.AppConfig{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ch := NewHTTPChannel(portCfg, appCfg, nil, logger)

	methods := []string{"GET", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"}
	for _, method := range methods {
		req := httptest.NewRequest(method, "/test", nil)
		w := httptest.NewRecorder()

		ch.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want %d", method, w.Code, http.StatusMethodNotAllowed)
		}
	}

	stats := ch.GetStats()
	if stats.Errors != int64(len(methods)) {
		t.Errorf("Errors = %d, want %d", stats.Errors, len(methods))
	}
}

func TestHTTPChannelEmptyBody(t *testing.T) {
	portCfg := config.PortConfig{
		Type:            "http",
		Path:            "/test",
		SideDesignation: "A1",
	}
	appCfg := config.AppConfig{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ch := NewHTTPChannel(portCfg, appCfg, nil, logger)

	req := httptest.NewRequest("POST", "/test", strings.NewReader(""))
	w := httptest.NewRecorder()

	ch.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	stats := ch.GetStats()
	if stats.Errors != 1 {
		t.Errorf("Errors = %d, want 1", stats.Errors)
	}
}

func TestHTTPChannelBuildRecord(t *testing.T) {
	portCfg := config.PortConfig{
		Type:            "http",
		Path:            "/test",
		SideDesignation: "A1",
	}
	appCfg := config.AppConfig{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ch := NewHTTPChannel(portCfg, appCfg, nil, logger)

	body := []byte("<xml>test</xml>")
	req := httptest.NewRequest("POST", "/test/endpoint?query=1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/xml")
	req.Header.Set("User-Agent", "TestClient/1.0")

	record := ch.buildRecord(req, body)

	// Check request line
	if !strings.Contains(record, "POST /test/endpoint?query=1") {
		t.Error("record should contain request line")
	}

	// Check headers
	if !strings.Contains(record, "Content-Type: application/xml") {
		t.Error("record should contain Content-Type header")
	}
	if !strings.Contains(record, "User-Agent: TestClient/1.0") {
		t.Error("record should contain User-Agent header")
	}
	if !strings.Contains(record, "X-Remote-Addr:") {
		t.Error("record should contain X-Remote-Addr header")
	}

	// Check body
	if !strings.Contains(record, "<xml>test</xml>") {
		t.Error("record should contain body")
	}

	// Check blank line between headers and body
	if !strings.Contains(record, "\n\n") {
		t.Error("record should have blank line between headers and body")
	}
}

func TestHTTPChannelStatsUpdates(t *testing.T) {
	// Test that stats are properly updated after requests
	// Note: This test doesn't use a real DualWriter, so we can't test
	// the full flow, but we can verify the stats tracking logic
	portCfg := config.PortConfig{
		Type:            "http",
		Path:            "/test",
		SideDesignation: "A1",
	}
	appCfg := config.AppConfig{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	ch := NewHTTPChannel(portCfg, appCfg, nil, logger)

	// Directly test atomic counters
	ch.bytesRead.Add(100)
	ch.requestCount.Add(2)
	ch.errorCount.Add(1)
	ch.statsMutex.Lock()
	ch.stats.LastRequestTime = time.Now()
	ch.statsMutex.Unlock()

	stats := ch.GetStats()
	if stats.BytesRead != 100 {
		t.Errorf("BytesRead = %d, want 100", stats.BytesRead)
	}
	if stats.RequestCount != 2 {
		t.Errorf("RequestCount = %d, want 2", stats.RequestCount)
	}
	if stats.Errors != 1 {
		t.Errorf("Errors = %d, want 1", stats.Errors)
	}
	if stats.LastRequestTime.IsZero() {
		t.Error("LastRequestTime should not be zero")
	}
}

func TestMaxHTTPBodySize(t *testing.T) {
	// Verify the constant is set to 50MB
	expected := int64(50 * 1024 * 1024)
	if MaxHTTPBodySize != expected {
		t.Errorf("MaxHTTPBodySize = %d, want %d (50MB)", MaxHTTPBodySize, expected)
	}
}
