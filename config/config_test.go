package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadValidConfig(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	configJSON := `{
		"app": {
			"name": "TestCollector",
			"instance_id": "test-01",
			"fips_code": "1234567890"
		},
		"ports": [
			{
				"device": "/dev/ttyS1",
				"side_designation": "A1",
				"baud_rate": 9600,
				"enabled": true
			}
		],
		"detection": {
			"baud_rates": [9600, 19200],
			"detection_timeout_sec": 5,
			"min_bytes_for_valid": 50
		},
		"nats": {
			"url": "nats://localhost:4222",
			"subject_prefix": "test.cdr",
			"max_reconnects": -1,
			"reconnect_wait_sec": 5
		},
		"logging": {
			"base_path": "` + tmpDir + `",
			"max_size_mb": 10,
			"max_backups": 3,
			"level": "info"
		},
		"monitoring": {
			"port": 8080
		},
		"recovery": {
			"reconnect_delay_sec": 5,
			"max_reconnect_delay_sec": 300
		}
	}`

	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Verify loaded values
	if cfg.App.Name != "TestCollector" {
		t.Errorf("App.Name = %q, want %q", cfg.App.Name, "TestCollector")
	}
	if cfg.App.InstanceID != "test-01" {
		t.Errorf("App.InstanceID = %q, want %q", cfg.App.InstanceID, "test-01")
	}
	if cfg.App.FIPSCode != "1234567890" {
		t.Errorf("App.FIPSCode = %q, want %q", cfg.App.FIPSCode, "1234567890")
	}
	if len(cfg.Ports) != 1 {
		t.Errorf("len(Ports) = %d, want 1", len(cfg.Ports))
	}
	if cfg.Ports[0].Device != "/dev/ttyS1" {
		t.Errorf("Ports[0].Device = %q, want %q", cfg.Ports[0].Device, "/dev/ttyS1")
	}
	if cfg.NATS.MaxReconnects != -1 {
		t.Errorf("NATS.MaxReconnects = %d, want -1", cfg.NATS.MaxReconnects)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.json")
	if err == nil {
		t.Error("Load() expected error for missing file, got nil")
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.json")

	if err := os.WriteFile(configPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	_, err := Load(configPath)
	if err == nil {
		t.Error("Load() expected error for invalid JSON, got nil")
	}
}

func TestDetectionConfigTimeout(t *testing.T) {
	cfg := DetectionConfig{
		DetectionTimeoutSec: 10,
	}

	timeout := cfg.DetectionTimeout()
	if timeout.Seconds() != 10 {
		t.Errorf("DetectionTimeout() = %v, want 10s", timeout)
	}
}

func TestRecoveryConfigDelays(t *testing.T) {
	cfg := RecoveryConfig{
		ReconnectDelaySec:    5,
		MaxReconnectDelaySec: 300,
	}

	if cfg.ReconnectDelay().Seconds() != 5 {
		t.Errorf("ReconnectDelay() = %v, want 5s", cfg.ReconnectDelay())
	}
	if cfg.MaxReconnectDelay().Seconds() != 300 {
		t.Errorf("MaxReconnectDelay() = %v, want 300s", cfg.MaxReconnectDelay())
	}
}
