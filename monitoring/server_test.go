package monitoring

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"nectarcollector/capture"
	"nectarcollector/config"
)

func newTestManager() *capture.Manager {
	cfg := &config.Config{
		App: config.AppConfig{
			Name:       "Test",
			InstanceID: "test-01",
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return capture.NewManager(cfg, "", logger)
}

func TestNewServer(t *testing.T) {
	cfg := &config.MonitoringConfig{
		Port:     8080,
		Username: "admin",
		Password: "secret",
	}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	server := NewServer(cfg, manager, "/var/log", logger)

	if server == nil {
		t.Fatal("NewServer() returned nil")
	}
	if server.config != cfg {
		t.Error("Server config not set correctly")
	}
	if server.manager != manager {
		t.Error("Server manager not set correctly")
	}
	if server.logBasePath != "/var/log" {
		t.Errorf("Server logBasePath = %q, want %q", server.logBasePath, "/var/log")
	}
}

func TestHandleHealth(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("GET", "/api/health", nil)
	rr := httptest.NewRecorder()

	server.handleHealth(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleHealth() status = %d, want %d", rr.Code, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("status = %v, want %q", response["status"], "healthy")
	}
	if _, ok := response["timestamp"]; !ok {
		t.Error("Response should include timestamp")
	}
}

func TestHandleStats(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("GET", "/api/stats", nil)
	rr := httptest.NewRecorder()

	server.handleStats(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleStats() status = %d, want %d", rr.Code, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["instance_id"] != "test-01" {
		t.Errorf("instance_id = %v, want %q", response["instance_id"], "test-01")
	}
}

func TestHandleFeedMissingChannel(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("GET", "/api/feed", nil)
	rr := httptest.NewRecorder()

	server.handleFeed(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("handleFeed() status = %d, want %d", rr.Code, http.StatusBadRequest)
	}
}

func TestHandleFeedWithChannel(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, tmpDir, logger)

	// Create a test log file
	logContent := "line1\nline2\nline3\n"
	logPath := filepath.Join(tmpDir, "test-channel.log")
	if err := os.WriteFile(logPath, []byte(logContent), 0644); err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/feed?channel=test-channel", nil)
	rr := httptest.NewRecorder()

	server.handleFeed(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleFeed() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["channel"] != "test-channel" {
		t.Errorf("channel = %v, want %q", response["channel"], "test-channel")
	}

	lines, ok := response["lines"].([]interface{})
	if !ok {
		t.Fatal("lines should be an array")
	}
	if len(lines) != 3 {
		t.Errorf("len(lines) = %d, want 3", len(lines))
	}
}

func TestHandleFeedWithCount(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, tmpDir, logger)

	// Create a test log file with more lines
	var content string
	for i := 1; i <= 100; i++ {
		content += "line\n"
	}
	logPath := filepath.Join(tmpDir, "big-channel.log")
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/feed?channel=big-channel&count=10", nil)
	rr := httptest.NewRecorder()

	server.handleFeed(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleFeed() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	lines, ok := response["lines"].([]interface{})
	if !ok {
		t.Fatal("lines should be an array")
	}
	if len(lines) != 10 {
		t.Errorf("len(lines) = %d, want 10", len(lines))
	}
}

func TestHandleFeedMaxCount(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, tmpDir, logger)

	// Create a test log file with many lines
	var content string
	for i := 1; i <= 500; i++ {
		content += "line\n"
	}
	logPath := filepath.Join(tmpDir, "huge-channel.log")
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test log file: %v", err)
	}

	// Request more than max (200)
	req := httptest.NewRequest("GET", "/api/feed?channel=huge-channel&count=999", nil)
	rr := httptest.NewRecorder()

	server.handleFeed(rr, req)

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	lines, ok := response["lines"].([]interface{})
	if !ok {
		t.Fatal("lines should be an array")
	}
	// Should be capped at 200
	if len(lines) != 200 {
		t.Errorf("len(lines) = %d, want 200 (max)", len(lines))
	}
}

func TestHandleFeedNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, tmpDir, logger)

	req := httptest.NewRequest("GET", "/api/feed?channel=nonexistent", nil)
	rr := httptest.NewRecorder()

	server.handleFeed(rr, req)

	// Should still return 200 with empty lines
	if rr.Code != http.StatusOK {
		t.Errorf("handleFeed() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	lines, ok := response["lines"].([]interface{})
	if !ok {
		t.Fatal("lines should be an array")
	}
	if len(lines) != 0 {
		t.Errorf("len(lines) = %d, want 0", len(lines))
	}
}

func TestHandleDashboard(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	server.handleDashboard(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleDashboard() status = %d, want %d", rr.Code, http.StatusOK)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "text/html" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/html")
	}

	// Should contain some HTML
	body := rr.Body.String()
	if len(body) < 100 {
		t.Error("Dashboard HTML should be non-trivial")
	}
}

func TestBasicAuth(t *testing.T) {
	cfg := &config.MonitoringConfig{
		Port:     8080,
		Username: "admin",
		Password: "secret",
	}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	// Create a handler wrapped with basic auth
	handler := server.basicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	}))

	tests := []struct {
		name       string
		user       string
		pass       string
		wantStatus int
	}{
		{"no auth", "", "", http.StatusUnauthorized},
		{"wrong user", "wrong", "secret", http.StatusUnauthorized},
		{"wrong pass", "admin", "wrong", http.StatusUnauthorized},
		{"correct auth", "admin", "secret", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.user != "" || tt.pass != "" {
				req.SetBasicAuth(tt.user, tt.pass)
			}
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rr.Code, tt.wantStatus)
			}
		})
	}
}

func TestBasicAuthWWWAuthenticate(t *testing.T) {
	cfg := &config.MonitoringConfig{
		Port:     8080,
		Username: "admin",
		Password: "secret",
	}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	handler := server.basicAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	wwwAuth := rr.Header().Get("WWW-Authenticate")
	if wwwAuth == "" {
		t.Error("WWW-Authenticate header should be set")
	}
	if wwwAuth != `Basic realm="NectarCollector"` {
		t.Errorf("WWW-Authenticate = %q, want %q", wwwAuth, `Basic realm="NectarCollector"`)
	}
}

func TestTailFile(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name    string
		content string
		n       int
		want    []string
	}{
		{
			name:    "fewer lines than requested",
			content: "a\nb\n",
			n:       5,
			want:    []string{"a", "b"},
		},
		{
			name:    "exact lines",
			content: "a\nb\nc\n",
			n:       3,
			want:    []string{"a", "b", "c"},
		},
		{
			name:    "more lines than requested",
			content: "a\nb\nc\nd\ne\n",
			n:       3,
			want:    []string{"c", "d", "e"},
		},
		{
			name:    "single line",
			content: "only\n",
			n:       10,
			want:    []string{"only"},
		},
		{
			name:    "empty file",
			content: "",
			n:       10,
			want:    []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(tmpDir, tt.name+".log")
			if err := os.WriteFile(path, []byte(tt.content), 0644); err != nil {
				t.Fatalf("Failed to write test file: %v", err)
			}

			got, err := tailFile(path, tt.n)
			if err != nil {
				t.Fatalf("tailFile() error = %v", err)
			}

			if len(got) != len(tt.want) {
				t.Errorf("len(tailFile()) = %d, want %d", len(got), len(tt.want))
				return
			}

			for i, want := range tt.want {
				if got[i] != want {
					t.Errorf("tailFile()[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

func TestTailFileNonExistent(t *testing.T) {
	_, err := tailFile("/nonexistent/file.log", 10)
	if err == nil {
		t.Error("tailFile() should return error for non-existent file")
	}
}

func BenchmarkTailFile(b *testing.B) {
	tmpDir := b.TempDir()
	path := filepath.Join(tmpDir, "bench.log")

	// Create a file with 10000 lines
	var content string
	for i := 0; i < 10000; i++ {
		content += "This is a sample log line for benchmarking purposes\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		b.Fatalf("Failed to write test file: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tailFile(path, 50)
	}
}

func newTestManagerWithPorts() *capture.Manager {
	cfg := &config.Config{
		App: config.AppConfig{
			Name:       "Test",
			InstanceID: "test-01",
			FIPSCode:   "3100000000",
		},
		Ports: []config.PortConfig{
			{
				Type:         "serial",
				Device:       "/dev/ttyS1",
				ADesignation: "A1",
				BaudRate:     9600,
				Enabled:      true,
			},
			{
				Type:         "http",
				Path:         "/cdr",
				ADesignation: "B1",
				Enabled:      true,
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	return capture.NewManager(cfg, "", logger)
}

func TestHandlePortsConfig(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManagerWithPorts()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("GET", "/api/ports/config", nil)
	rr := httptest.NewRecorder()

	server.handlePortsConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handlePortsConfig() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	ports, ok := response["ports"].([]interface{})
	if !ok {
		t.Fatal("ports should be an array")
	}
	if len(ports) != 2 {
		t.Errorf("len(ports) = %d, want 2", len(ports))
	}
}

func TestHandlePortConfigGetNotFound(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("GET", "/api/ports/config/nonexistent", nil)
	rr := httptest.NewRecorder()

	server.handlePortGet(rr, req, "nonexistent")

	if rr.Code != http.StatusNotFound {
		t.Errorf("handlePortGet() status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHandlePortEnableNotFound(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("POST", "/api/ports/config/nonexistent/enable", nil)
	rr := httptest.NewRecorder()

	server.handlePortEnable(rr, req, "nonexistent")

	if rr.Code != http.StatusNotFound {
		t.Errorf("handlePortEnable() status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHandlePortDisableNotFound(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("POST", "/api/ports/config/nonexistent/disable", nil)
	rr := httptest.NewRecorder()

	server.handlePortDisable(rr, req, "nonexistent")

	if rr.Code != http.StatusNotFound {
		t.Errorf("handlePortDisable() status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHandlePortDeleteNotFound(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("DELETE", "/api/ports/config/nonexistent", nil)
	rr := httptest.NewRecorder()

	server.handlePortDelete(rr, req, "nonexistent")

	if rr.Code != http.StatusNotFound {
		t.Errorf("handlePortDelete() status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestHandleAvailablePorts(t *testing.T) {
	cfg := &config.MonitoringConfig{Port: 8080}
	manager := newTestManager()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	server := NewServer(cfg, manager, "/var/log", logger)

	req := httptest.NewRequest("GET", "/api/ports/available", nil)
	rr := httptest.NewRecorder()

	server.handleAvailablePorts(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("handleAvailablePorts() status = %d, want %d", rr.Code, http.StatusOK)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have available_ports key
	if _, ok := response["available_ports"]; !ok {
		t.Error("Response should have available_ports key")
	}
}

func TestValidatePortUpdates(t *testing.T) {
	tests := []struct {
		name    string
		updates map[string]interface{}
		wantErr bool
	}{
		{
			name:    "empty updates",
			updates: map[string]interface{}{},
			wantErr: false,
		},
		{
			name: "valid baud rate",
			updates: map[string]interface{}{
				"baud_rate": float64(9600),
			},
			wantErr: false,
		},
		{
			name: "invalid baud rate",
			updates: map[string]interface{}{
				"baud_rate": float64(12345),
			},
			wantErr: true,
		},
		{
			name: "valid data bits",
			updates: map[string]interface{}{
				"data_bits": float64(8),
			},
			wantErr: false,
		},
		{
			name: "invalid data bits",
			updates: map[string]interface{}{
				"data_bits": float64(9),
			},
			wantErr: true,
		},
		{
			name: "valid parity",
			updates: map[string]interface{}{
				"parity": "none",
			},
			wantErr: false,
		},
		{
			name: "invalid parity",
			updates: map[string]interface{}{
				"parity": "invalid",
			},
			wantErr: true,
		},
		{
			name: "valid stop bits",
			updates: map[string]interface{}{
				"stop_bits": float64(1),
			},
			wantErr: false,
		},
		{
			name: "invalid stop bits",
			updates: map[string]interface{}{
				"stop_bits": float64(3),
			},
			wantErr: true,
		},
		{
			name: "valid path",
			updates: map[string]interface{}{
				"path": "/newpath",
			},
			wantErr: false,
		},
		{
			name: "invalid path",
			updates: map[string]interface{}{
				"path": "noSlash",
			},
			wantErr: true,
		},
		{
			name: "valid listen port",
			updates: map[string]interface{}{
				"listen_port": float64(8080),
			},
			wantErr: false,
		},
		{
			name: "invalid listen port too high",
			updates: map[string]interface{}{
				"listen_port": float64(70000),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePortUpdates(tt.updates)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePortUpdates() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
