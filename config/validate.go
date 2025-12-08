package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	// Valid baud rates
	validBaudRates = map[int]bool{
		300:    true,
		1200:   true,
		2400:   true,
		4800:   true,
		9600:   true,
		19200:  true,
		38400:  true,
		57600:  true,
		115200: true,
	}

	// Valid log levels
	validLogLevels = map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}

	// A designation pattern: A1 through A16
	aDesignationPattern = regexp.MustCompile(`^A([1-9]|1[0-6])$`)

	// FIPS code pattern: 10 digits
	fipsCodePattern = regexp.MustCompile(`^\d{10}$`)
)

// Validate performs comprehensive validation of the configuration
func (c *Config) Validate() error {
	if err := c.validateApp(); err != nil {
		return fmt.Errorf("app config: %w", err)
	}

	if err := c.validatePorts(); err != nil {
		return fmt.Errorf("ports config: %w", err)
	}

	if err := c.validateDetection(); err != nil {
		return fmt.Errorf("detection config: %w", err)
	}

	if err := c.validateNATS(); err != nil {
		return fmt.Errorf("nats config: %w", err)
	}

	if err := c.validateLogging(); err != nil {
		return fmt.Errorf("logging config: %w", err)
	}

	if err := c.validateMonitoring(); err != nil {
		return fmt.Errorf("monitoring config: %w", err)
	}

	if err := c.validateRecovery(); err != nil {
		return fmt.Errorf("recovery config: %w", err)
	}

	return nil
}

func (c *Config) validateApp() error {
	if c.App.Name == "" {
		return fmt.Errorf("name is required")
	}

	if c.App.InstanceID == "" {
		return fmt.Errorf("instance_id is required")
	}

	if c.App.FIPSCode != "" && !fipsCodePattern.MatchString(c.App.FIPSCode) {
		return fmt.Errorf("fips_code must be 10 digits, got: %s", c.App.FIPSCode)
	}

	return nil
}

func (c *Config) validatePorts() error {
	if len(c.Ports) == 0 {
		return fmt.Errorf("at least one port must be configured")
	}

	enabledCount := 0
	devicesSeen := make(map[string]bool)
	aDesignationsSeen := make(map[string]bool)

	for i, port := range c.Ports {
		// Check device
		if port.Device == "" {
			return fmt.Errorf("port %d: device is required", i)
		}

		// Check for duplicate devices
		if devicesSeen[port.Device] {
			return fmt.Errorf("port %d: duplicate device %s", i, port.Device)
		}
		devicesSeen[port.Device] = true

		// Check A designation
		if port.ADesignation == "" {
			return fmt.Errorf("port %d (%s): a_designation is required", i, port.Device)
		}
		if !aDesignationPattern.MatchString(port.ADesignation) {
			return fmt.Errorf("port %d (%s): a_designation must be A1-A16, got: %s", i, port.Device, port.ADesignation)
		}

		// Check for duplicate A designations (among enabled ports)
		if port.Enabled && aDesignationsSeen[port.ADesignation] {
			return fmt.Errorf("port %d (%s): duplicate a_designation %s among enabled ports", i, port.Device, port.ADesignation)
		}
		if port.Enabled {
			aDesignationsSeen[port.ADesignation] = true
		}

		// Validate baud rate if specified
		if port.BaudRate != 0 && !validBaudRates[port.BaudRate] {
			return fmt.Errorf("port %d (%s): invalid baud_rate %d, must be one of: 300, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200",
				i, port.Device, port.BaudRate)
		}

		// Validate FIPS code if specified
		if port.FIPSCode != "" && !fipsCodePattern.MatchString(port.FIPSCode) {
			return fmt.Errorf("port %d (%s): fips_code must be 10 digits, got: %s", i, port.Device, port.FIPSCode)
		}

		if port.Enabled {
			enabledCount++
		}
	}

	if enabledCount == 0 {
		return fmt.Errorf("at least one port must be enabled")
	}

	return nil
}

func (c *Config) validateDetection() error {
	if len(c.Detection.BaudRates) == 0 {
		return fmt.Errorf("at least one baud rate must be configured")
	}

	for _, baudRate := range c.Detection.BaudRates {
		if !validBaudRates[baudRate] {
			return fmt.Errorf("invalid baud rate %d in detection config", baudRate)
		}
	}

	if c.Detection.DetectionTimeoutSec <= 0 {
		return fmt.Errorf("detection_timeout_sec must be positive, got: %d", c.Detection.DetectionTimeoutSec)
	}

	if c.Detection.MinBytesForValid <= 0 {
		return fmt.Errorf("min_bytes_for_valid must be positive, got: %d", c.Detection.MinBytesForValid)
	}

	return nil
}

func (c *Config) validateNATS() error {
	if c.NATS.URL == "" {
		return fmt.Errorf("url is required")
	}

	if !strings.HasPrefix(c.NATS.URL, "nats://") {
		return fmt.Errorf("url must start with nats://, got: %s", c.NATS.URL)
	}

	if c.NATS.SubjectPrefix == "" {
		return fmt.Errorf("subject_prefix is required")
	}

	// -1 means unlimited reconnects (NATS client convention)
	if c.NATS.MaxReconnects < -1 {
		return fmt.Errorf("max_reconnects must be -1 (unlimited) or non-negative, got: %d", c.NATS.MaxReconnects)
	}

	if c.NATS.ReconnectWaitSec <= 0 {
		return fmt.Errorf("reconnect_wait_sec must be positive, got: %d", c.NATS.ReconnectWaitSec)
	}

	return nil
}

func (c *Config) validateLogging() error {
	if c.Logging.BasePath == "" {
		return fmt.Errorf("base_path is required")
	}

	// Check if base path exists or can be created
	if _, err := os.Stat(c.Logging.BasePath); os.IsNotExist(err) {
		// Try to create it
		if err := os.MkdirAll(c.Logging.BasePath, 0755); err != nil {
			return fmt.Errorf("base_path %s does not exist and cannot be created: %w", c.Logging.BasePath, err)
		}
	}

	if c.Logging.MaxSizeMB <= 0 {
		return fmt.Errorf("max_size_mb must be positive, got: %d", c.Logging.MaxSizeMB)
	}

	if c.Logging.MaxBackups < 0 {
		return fmt.Errorf("max_backups must be non-negative, got: %d", c.Logging.MaxBackups)
	}

	if !validLogLevels[c.Logging.Level] {
		return fmt.Errorf("invalid log level %s, must be one of: debug, info, warn, error", c.Logging.Level)
	}

	return nil
}

func (c *Config) validateMonitoring() error {
	if c.Monitoring.Port <= 0 || c.Monitoring.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got: %d", c.Monitoring.Port)
	}

	return nil
}

func (c *Config) validateRecovery() error {
	if c.Recovery.ReconnectDelaySec <= 0 {
		return fmt.Errorf("reconnect_delay_sec must be positive, got: %d", c.Recovery.ReconnectDelaySec)
	}

	if c.Recovery.MaxReconnectDelaySec <= 0 {
		return fmt.Errorf("max_reconnect_delay_sec must be positive, got: %d", c.Recovery.MaxReconnectDelaySec)
	}

	if c.Recovery.MaxReconnectDelaySec < c.Recovery.ReconnectDelaySec {
		return fmt.Errorf("max_reconnect_delay_sec (%d) must be >= reconnect_delay_sec (%d)",
			c.Recovery.MaxReconnectDelaySec, c.Recovery.ReconnectDelaySec)
	}

	return nil
}
