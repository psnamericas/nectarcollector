package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// Config is the root configuration structure
type Config struct {
	App        AppConfig        `json:"app"`
	Ports      []PortConfig     `json:"ports"`
	Detection  DetectionConfig  `json:"detection"`
	NATS       NATSConfig       `json:"nats"`
	Logging    LoggingConfig    `json:"logging"`
	Monitoring MonitoringConfig `json:"monitoring"`
	Recovery   RecoveryConfig   `json:"recovery"`
}

// AppConfig contains application-level settings
type AppConfig struct {
	Name       string `json:"name"`
	InstanceID string `json:"instance_id"`
	FIPSCode   string `json:"fips_code"` // Default FIPS code for all ports
}

// PortConfig defines configuration for a single serial port
type PortConfig struct {
	Device         string `json:"device"`           // e.g., "/dev/ttyUSB0"
	ADesignation   string `json:"a_designation"`    // "A1" through "A16"
	FIPSCode       string `json:"fips_code"`        // Optional override for this port
	Vendor         string `json:"vendor"`           // CPE vendor: "intrado", "solacom", "zetron", "vesta", etc.
	County         string `json:"county"`           // County name (lowercase): "lancaster", "douglas", etc.
	BaudRate       int    `json:"baud_rate"`        // 0 = auto-detect
	DataBits       int    `json:"data_bits"`        // 5, 6, 7, or 8 (default: 8)
	Parity         string `json:"parity"`           // "none", "odd", "even", "mark", "space" (default: "none")
	StopBits       int    `json:"stop_bits"`        // 1 or 2 (default: 1)
	UseFlowControl *bool  `json:"use_flow_control"` // nil = auto-detect
	Enabled        bool   `json:"enabled"`
	Description    string `json:"description"`
}

// DetectionConfig contains parameters for autobaud and pinout detection
type DetectionConfig struct {
	BaudRates           []int `json:"baud_rates"`            // List of baud rates to try
	DetectionTimeoutSec int   `json:"detection_timeout_sec"` // Timeout per detection attempt
	MinBytesForValid    int   `json:"min_bytes_for_valid"`   // Minimum bytes to consider valid
}

// NATSConfig contains NATS JetStream connection settings
type NATSConfig struct {
	URL              string `json:"url"`                // NATS server URL
	SubjectPrefix    string `json:"subject_prefix"`     // Prefix for subjects (e.g., "serial")
	MaxReconnects    int    `json:"max_reconnects"`     // Max reconnection attempts
	ReconnectWaitSec int    `json:"reconnect_wait_sec"` // Wait between reconnects
}

// LoggingConfig contains logging and log rotation settings
type LoggingConfig struct {
	BasePath   string `json:"base_path"`   // Base directory for log files
	MaxSizeMB  int    `json:"max_size_mb"` // Max size before rotation
	MaxBackups int    `json:"max_backups"` // Max number of old log files
	Compress   bool   `json:"compress"`    // Compress rotated logs
	Level      string `json:"level"`       // Log level: debug, info, warn, error
}

// MonitoringConfig contains HTTP monitoring server settings
type MonitoringConfig struct {
	Port     int    `json:"port"`     // HTTP port for monitoring endpoints
	Username string `json:"username"` // Basic auth username (empty = no auth)
	Password string `json:"password"` // Basic auth password
}

// RecoveryConfig contains reconnection and recovery settings
type RecoveryConfig struct {
	ReconnectDelaySec    int  `json:"reconnect_delay_sec"`     // Initial reconnect delay
	MaxReconnectDelaySec int  `json:"max_reconnect_delay_sec"` // Maximum reconnect delay
	ExponentialBackoff   bool `json:"exponential_backoff"`     // Use exponential backoff
}

// Load reads and parses the configuration file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Set defaults
	cfg.setDefaults()

	// Validate
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &cfg, nil
}

// setDefaults fills in default values for optional fields
func (c *Config) setDefaults() {
	// App defaults
	if c.App.Name == "" {
		c.App.Name = "NectarCollector"
	}
	if c.App.InstanceID == "" {
		c.App.InstanceID = "default"
	}

	// Detection defaults
	// Prioritize common PSAP/CHE equipment baud rates:
	// - 9600: Most common (Vesta, Viper, Positron default)
	// - 19200: Second most common (some Viper configs)
	// - 4800: Legacy equipment
	// - 38400: Some newer Vesta installs
	// Then fall back to other standard rates
	if len(c.Detection.BaudRates) == 0 {
		c.Detection.BaudRates = []int{9600, 19200, 4800, 38400, 115200, 57600, 2400, 1200, 300}
	}
	if c.Detection.DetectionTimeoutSec == 0 {
		c.Detection.DetectionTimeoutSec = 2 // 2 seconds per baud rate (was 5)
	}
	if c.Detection.MinBytesForValid == 0 {
		c.Detection.MinBytesForValid = 50
	}

	// NATS defaults
	if c.NATS.URL == "" {
		c.NATS.URL = "nats://localhost:4222"
	}
	if c.NATS.SubjectPrefix == "" {
		c.NATS.SubjectPrefix = "serial"
	}
	if c.NATS.MaxReconnects == 0 {
		c.NATS.MaxReconnects = 10
	}
	if c.NATS.ReconnectWaitSec == 0 {
		c.NATS.ReconnectWaitSec = 5
	}

	// Logging defaults
	if c.Logging.BasePath == "" {
		c.Logging.BasePath = "/var/log/nectarcollector"
	}
	if c.Logging.MaxSizeMB == 0 {
		c.Logging.MaxSizeMB = 50
	}
	if c.Logging.MaxBackups == 0 {
		c.Logging.MaxBackups = 10
	}
	// Compress defaults to true via JSON unmarshaling (zero value is false, but we
	// don't override here so users can explicitly set compress: false in config)
	// Note: To default to true, we'd need a *bool, but for simplicity we accept
	// that omitting "compress" means false (Go's zero value). Users wanting
	// compression should explicitly set compress: true.
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}

	// Monitoring defaults
	if c.Monitoring.Port == 0 {
		c.Monitoring.Port = 8080
	}

	// Recovery defaults
	if c.Recovery.ReconnectDelaySec == 0 {
		c.Recovery.ReconnectDelaySec = 1 // Fast initial retry
	}
	if c.Recovery.MaxReconnectDelaySec == 0 {
		c.Recovery.MaxReconnectDelaySec = 60 // Cap at 1 minute
	}
}

// Helper methods for time conversions
func (d *DetectionConfig) DetectionTimeout() time.Duration {
	return time.Duration(d.DetectionTimeoutSec) * time.Second
}

func (n *NATSConfig) ReconnectWait() time.Duration {
	return time.Duration(n.ReconnectWaitSec) * time.Second
}

func (r *RecoveryConfig) ReconnectDelay() time.Duration {
	return time.Duration(r.ReconnectDelaySec) * time.Second
}

func (r *RecoveryConfig) MaxReconnectDelay() time.Duration {
	return time.Duration(r.MaxReconnectDelaySec) * time.Second
}
