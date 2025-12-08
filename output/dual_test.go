package output

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewDualWriter(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg := &DualWriterConfig{
		Device:        "/dev/ttyS1",
		Identifier:    "1234567890-A1",
		LogBasePath:   tmpDir,
		LogMaxSizeMB:  10,
		LogMaxBackups: 3,
		LogCompress:   true,
		NATSConn:      nil, // No NATS for this test
		NATSSubject:   "test.cdr.subject",
		Logger:        logger,
	}

	dw, err := NewDualWriter(cfg)
	if err != nil {
		t.Fatalf("NewDualWriter() error = %v", err)
	}
	defer dw.Close()

	if dw.device != "/dev/ttyS1" {
		t.Errorf("device = %q, want %q", dw.device, "/dev/ttyS1")
	}
	if dw.natsSubject != "test.cdr.subject" {
		t.Errorf("natsSubject = %q, want %q", dw.natsSubject, "test.cdr.subject")
	}
	if dw.natsEnabled {
		t.Error("natsEnabled should be false when NATSConn is nil")
	}
}

func TestDualWriterWrite(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg := &DualWriterConfig{
		Device:        "/dev/ttyS1",
		Identifier:    "1234567890-A1",
		LogBasePath:   tmpDir,
		LogMaxSizeMB:  10,
		LogMaxBackups: 3,
		LogCompress:   false,
		NATSConn:      nil,
		NATSSubject:   "test.cdr",
		Logger:        logger,
	}

	dw, err := NewDualWriter(cfg)
	if err != nil {
		t.Fatalf("NewDualWriter() error = %v", err)
	}

	testData := "test line data"
	if err := dw.Write(testData); err != nil {
		t.Errorf("Write() error = %v", err)
	}

	dw.Close()

	// Verify file was written
	logPath := filepath.Join(tmpDir, "1234567890-A1.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	if string(content) != testData {
		t.Errorf("Log content = %q, want %q", string(content), testData)
	}
}

func TestDualWriterWriteLine(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg := &DualWriterConfig{
		Device:        "/dev/ttyS1",
		Identifier:    "test-id",
		LogBasePath:   tmpDir,
		LogMaxSizeMB:  10,
		LogMaxBackups: 3,
		LogCompress:   false,
		NATSConn:      nil,
		NATSSubject:   "test.cdr",
		Logger:        logger,
	}

	dw, err := NewDualWriter(cfg)
	if err != nil {
		t.Fatalf("NewDualWriter() error = %v", err)
	}

	// Test without trailing newline
	if err := dw.WriteLine("line without newline"); err != nil {
		t.Errorf("WriteLine() error = %v", err)
	}

	// Test with trailing newline
	if err := dw.WriteLine("line with newline\n"); err != nil {
		t.Errorf("WriteLine() error = %v", err)
	}

	dw.Close()

	// Verify file contents
	logPath := filepath.Join(tmpDir, "test-id.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	expected := "line without newline\nline with newline\n"
	if string(content) != expected {
		t.Errorf("Log content = %q, want %q", string(content), expected)
	}
}

func TestDualWriterMultipleWrites(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg := &DualWriterConfig{
		Device:        "/dev/ttyS1",
		Identifier:    "multi-test",
		LogBasePath:   tmpDir,
		LogMaxSizeMB:  10,
		LogMaxBackups: 3,
		LogCompress:   false,
		NATSConn:      nil,
		NATSSubject:   "test.cdr",
		Logger:        logger,
	}

	dw, err := NewDualWriter(cfg)
	if err != nil {
		t.Fatalf("NewDualWriter() error = %v", err)
	}

	lines := []string{
		"[1234567890][A1][2025-01-01 00:00:00.000] First line",
		"[1234567890][A1][2025-01-01 00:00:01.000] Second line",
		"[1234567890][A1][2025-01-01 00:00:02.000] Third line",
	}

	for _, line := range lines {
		if err := dw.WriteLine(line); err != nil {
			t.Errorf("WriteLine() error = %v", err)
		}
	}

	dw.Close()

	// Verify all lines were written
	logPath := filepath.Join(tmpDir, "multi-test.log")
	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	contentLines := strings.Split(strings.TrimSuffix(string(content), "\n"), "\n")
	if len(contentLines) != len(lines) {
		t.Errorf("Got %d lines, want %d", len(contentLines), len(lines))
	}

	for i, want := range lines {
		if i < len(contentLines) && contentLines[i] != want {
			t.Errorf("Line %d = %q, want %q", i, contentLines[i], want)
		}
	}
}

func TestDualWriterLogPath(t *testing.T) {
	tmpDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	tests := []struct {
		identifier string
		wantFile   string
	}{
		{"1234567890-A1", "1234567890-A1.log"},
		{"simple", "simple.log"},
		{"with-dashes-and-numbers-123", "with-dashes-and-numbers-123.log"},
	}

	for _, tt := range tests {
		t.Run(tt.identifier, func(t *testing.T) {
			cfg := &DualWriterConfig{
				Device:        "/dev/ttyS1",
				Identifier:    tt.identifier,
				LogBasePath:   tmpDir,
				LogMaxSizeMB:  10,
				LogMaxBackups: 3,
				LogCompress:   false,
				NATSConn:      nil,
				NATSSubject:   "test.cdr",
				Logger:        logger,
			}

			dw, err := NewDualWriter(cfg)
			if err != nil {
				t.Fatalf("NewDualWriter() error = %v", err)
			}

			dw.WriteLine("test")
			dw.Close()

			expectedPath := filepath.Join(tmpDir, tt.wantFile)
			if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
				t.Errorf("Expected log file %s to exist", expectedPath)
			}
		})
	}
}

func TestNATSConnectionIsConnected(t *testing.T) {
	// Test with nil connection
	nc := &NATSConnection{
		conn:   nil,
		url:    "nats://localhost:4222",
		logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	if nc.IsConnected() {
		t.Error("IsConnected() should return false when conn is nil")
	}
}

func TestNATSConnectionClose(t *testing.T) {
	nc := &NATSConnection{
		conn:   nil,
		url:    "nats://localhost:4222",
		logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}

	// Should not panic on nil connection
	nc.Close()

	// Should be safe to call multiple times
	nc.Close()
}

func BenchmarkDualWriterWriteLine(b *testing.B) {
	tmpDir := b.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg := &DualWriterConfig{
		Device:        "/dev/ttyS1",
		Identifier:    "bench-test",
		LogBasePath:   tmpDir,
		LogMaxSizeMB:  100,
		LogMaxBackups: 1,
		LogCompress:   false,
		NATSConn:      nil,
		NATSSubject:   "test.cdr",
		Logger:        logger,
	}

	dw, _ := NewDualWriter(cfg)
	defer dw.Close()

	testLine := "[1234567890][A1][2025-01-01 00:00:00.000] Sample CDR data line for benchmarking"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dw.WriteLine(testLine)
	}
}
