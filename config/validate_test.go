package config

import (
	"testing"
)

func validConfig(t *testing.T) *Config {
	t.Helper()
	tmpDir := t.TempDir()
	return &Config{
		App: AppConfig{
			Name:       "Test",
			InstanceID: "test-01",
			FIPSCode:   "1234567890",
		},
		Ports: []PortConfig{
			{
				Device:       "/dev/ttyS1",
				ADesignation: "A1",
				BaudRate:     9600,
				Enabled:      true,
			},
		},
		Detection: DetectionConfig{
			BaudRates:           []int{9600},
			DetectionTimeoutSec: 5,
			MinBytesForValid:    50,
		},
		NATS: NATSConfig{
			URL:              "nats://localhost:4222",
			SubjectPrefix:    "test.cdr",
			MaxReconnects:    -1,
			ReconnectWaitSec: 5,
		},
		Logging: LoggingConfig{
			BasePath:   tmpDir,
			MaxSizeMB:  10,
			MaxBackups: 3,
			Level:      "info",
		},
		Monitoring: MonitoringConfig{
			Port: 8080,
		},
		Recovery: RecoveryConfig{
			ReconnectDelaySec:    5,
			MaxReconnectDelaySec: 300,
		},
	}
}

func TestValidateValidConfig(t *testing.T) {
	cfg := validConfig(t)
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want nil", err)
	}
}

func TestValidateAppConfig(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid config",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "missing app name",
			modify:  func(c *Config) { c.App.Name = "" },
			wantErr: true,
		},
		{
			name:    "missing instance_id",
			modify:  func(c *Config) { c.App.InstanceID = "" },
			wantErr: true,
		},
		{
			name:    "invalid fips_code too short",
			modify:  func(c *Config) { c.App.FIPSCode = "12345" },
			wantErr: true,
		},
		{
			name:    "invalid fips_code non-numeric",
			modify:  func(c *Config) { c.App.FIPSCode = "123456789a" },
			wantErr: true,
		},
		{
			name:    "empty fips_code is valid",
			modify:  func(c *Config) { c.App.FIPSCode = "" },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePortConfig(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid port",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "no ports",
			modify:  func(c *Config) { c.Ports = nil },
			wantErr: true,
		},
		{
			name:    "no enabled ports",
			modify:  func(c *Config) { c.Ports[0].Enabled = false },
			wantErr: true,
		},
		{
			name:    "missing device",
			modify:  func(c *Config) { c.Ports[0].Device = "" },
			wantErr: true,
		},
		{
			name:    "missing a_designation",
			modify:  func(c *Config) { c.Ports[0].ADesignation = "" },
			wantErr: true,
		},
		{
			name:    "invalid a_designation A0",
			modify:  func(c *Config) { c.Ports[0].ADesignation = "A0" },
			wantErr: true,
		},
		{
			name:    "invalid a_designation A17",
			modify:  func(c *Config) { c.Ports[0].ADesignation = "A17" },
			wantErr: true,
		},
		{
			name:    "invalid a_designation B1",
			modify:  func(c *Config) { c.Ports[0].ADesignation = "B1" },
			wantErr: true,
		},
		{
			name:    "valid a_designation A16",
			modify:  func(c *Config) { c.Ports[0].ADesignation = "A16" },
			wantErr: false,
		},
		{
			name:    "invalid baud_rate",
			modify:  func(c *Config) { c.Ports[0].BaudRate = 12345 },
			wantErr: true,
		},
		{
			name:    "baud_rate 0 is valid (auto-detect)",
			modify:  func(c *Config) { c.Ports[0].BaudRate = 0 },
			wantErr: false,
		},
		{
			name: "duplicate device",
			modify: func(c *Config) {
				c.Ports = append(c.Ports, PortConfig{
					Device:       "/dev/ttyS1",
					ADesignation: "A2",
					Enabled:      true,
				})
			},
			wantErr: true,
		},
		{
			name: "duplicate a_designation among enabled",
			modify: func(c *Config) {
				c.Ports = append(c.Ports, PortConfig{
					Device:       "/dev/ttyS2",
					ADesignation: "A1",
					Enabled:      true,
				})
			},
			wantErr: true,
		},
		{
			name: "duplicate a_designation with one disabled is ok",
			modify: func(c *Config) {
				c.Ports = append(c.Ports, PortConfig{
					Device:       "/dev/ttyS2",
					ADesignation: "A1",
					Enabled:      false,
				})
			},
			wantErr: false,
		},
		{
			name:    "invalid port fips_code",
			modify:  func(c *Config) { c.Ports[0].FIPSCode = "invalid" },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateDetectionConfig(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid detection",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "no baud_rates",
			modify:  func(c *Config) { c.Detection.BaudRates = nil },
			wantErr: true,
		},
		{
			name:    "invalid baud_rate in list",
			modify:  func(c *Config) { c.Detection.BaudRates = []int{9600, 12345} },
			wantErr: true,
		},
		{
			name:    "zero detection_timeout",
			modify:  func(c *Config) { c.Detection.DetectionTimeoutSec = 0 },
			wantErr: true,
		},
		{
			name:    "negative detection_timeout",
			modify:  func(c *Config) { c.Detection.DetectionTimeoutSec = -1 },
			wantErr: true,
		},
		{
			name:    "zero min_bytes",
			modify:  func(c *Config) { c.Detection.MinBytesForValid = 0 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateNATSConfig(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid nats",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "missing url",
			modify:  func(c *Config) { c.NATS.URL = "" },
			wantErr: true,
		},
		{
			name:    "invalid url scheme",
			modify:  func(c *Config) { c.NATS.URL = "http://localhost:4222" },
			wantErr: true,
		},
		{
			name:    "missing subject_prefix",
			modify:  func(c *Config) { c.NATS.SubjectPrefix = "" },
			wantErr: true,
		},
		{
			name:    "max_reconnects -1 is valid (unlimited)",
			modify:  func(c *Config) { c.NATS.MaxReconnects = -1 },
			wantErr: false,
		},
		{
			name:    "max_reconnects 0 is valid",
			modify:  func(c *Config) { c.NATS.MaxReconnects = 0 },
			wantErr: false,
		},
		{
			name:    "max_reconnects -2 is invalid",
			modify:  func(c *Config) { c.NATS.MaxReconnects = -2 },
			wantErr: true,
		},
		{
			name:    "zero reconnect_wait",
			modify:  func(c *Config) { c.NATS.ReconnectWaitSec = 0 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateLoggingConfig(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid logging",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "missing base_path",
			modify:  func(c *Config) { c.Logging.BasePath = "" },
			wantErr: true,
		},
		{
			name:    "zero max_size_mb",
			modify:  func(c *Config) { c.Logging.MaxSizeMB = 0 },
			wantErr: true,
		},
		{
			name:    "negative max_backups",
			modify:  func(c *Config) { c.Logging.MaxBackups = -1 },
			wantErr: true,
		},
		{
			name:    "invalid log level",
			modify:  func(c *Config) { c.Logging.Level = "trace" },
			wantErr: true,
		},
		{
			name:    "valid debug level",
			modify:  func(c *Config) { c.Logging.Level = "debug" },
			wantErr: false,
		},
		{
			name:    "valid warn level",
			modify:  func(c *Config) { c.Logging.Level = "warn" },
			wantErr: false,
		},
		{
			name:    "valid error level",
			modify:  func(c *Config) { c.Logging.Level = "error" },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMonitoringConfig(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid monitoring",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "zero port",
			modify:  func(c *Config) { c.Monitoring.Port = 0 },
			wantErr: true,
		},
		{
			name:    "port too high",
			modify:  func(c *Config) { c.Monitoring.Port = 65536 },
			wantErr: true,
		},
		{
			name:    "port 65535 is valid",
			modify:  func(c *Config) { c.Monitoring.Port = 65535 },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRecoveryConfig(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid recovery",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "zero reconnect_delay",
			modify:  func(c *Config) { c.Recovery.ReconnectDelaySec = 0 },
			wantErr: true,
		},
		{
			name:    "zero max_reconnect_delay",
			modify:  func(c *Config) { c.Recovery.MaxReconnectDelaySec = 0 },
			wantErr: true,
		},
		{
			name: "max less than initial",
			modify: func(c *Config) {
				c.Recovery.ReconnectDelaySec = 10
				c.Recovery.MaxReconnectDelaySec = 5
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig(t)
			tt.modify(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidBaudRates(t *testing.T) {
	validRates := []int{300, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200}

	for _, rate := range validRates {
		if !validBaudRates[rate] {
			t.Errorf("Expected %d to be a valid baud rate", rate)
		}
	}

	invalidRates := []int{0, 100, 1000, 9601, 100000}
	for _, rate := range invalidRates {
		if validBaudRates[rate] {
			t.Errorf("Expected %d to be an invalid baud rate", rate)
		}
	}
}

func TestADesignationPattern(t *testing.T) {
	valid := []string{"A1", "A2", "A9", "A10", "A15", "A16"}
	for _, s := range valid {
		if !aDesignationPattern.MatchString(s) {
			t.Errorf("Expected %q to be a valid A designation", s)
		}
	}

	invalid := []string{"A0", "A17", "A100", "B1", "a1", "1A", "A", ""}
	for _, s := range invalid {
		if aDesignationPattern.MatchString(s) {
			t.Errorf("Expected %q to be an invalid A designation", s)
		}
	}
}

func TestFIPSCodePattern(t *testing.T) {
	valid := []string{"0000000000", "1234567890", "9999999999"}
	for _, s := range valid {
		if !fipsCodePattern.MatchString(s) {
			t.Errorf("Expected %q to be a valid FIPS code", s)
		}
	}

	invalid := []string{"", "123456789", "12345678901", "123456789a", "abcdefghij"}
	for _, s := range invalid {
		if fipsCodePattern.MatchString(s) {
			t.Errorf("Expected %q to be an invalid FIPS code", s)
		}
	}
}
