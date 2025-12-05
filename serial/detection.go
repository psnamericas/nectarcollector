package serial

import (
	"fmt"
	"io"
	"log/slog"
	"time"
)

// Detection constants - these control autobaud detection behavior
const (
	// DetectionSettlingTime is the delay between detection cycles to allow
	// USB-to-serial adapters and other hardware to stabilize after close/reopen.
	// This follows RS-232 best practices for reliable communication.
	DetectionSettlingTime = 100 * time.Millisecond

	// DetectionBufferSize is the size of the buffer used when sampling
	// data during baud rate detection.
	DetectionBufferSize = 4096

	// FlowControlTestBufferSize is the smaller buffer used during pinout detection.
	FlowControlTestBufferSize = 1024

	// DetectionPollInterval is the sleep time between read attempts during
	// detection to avoid busy-looping while waiting for data.
	DetectionPollInterval = 10 * time.Millisecond

	// ValidityThreshold is the minimum ratio of valid ASCII characters
	// required for a baud rate to be considered correct (0.80 = 80%).
	ValidityThreshold = 0.80
)

// DetectionResult contains the results of autobaud and pinout detection
type DetectionResult struct {
	BaudRate       int
	UseFlowControl bool
	ValidityRatio  float64
	BytesRead      int
}

// Detector handles autobaud and pinout detection
type Detector struct {
	device           string
	baudRates        []int
	detectionTimeout time.Duration
	minBytesForValid int
	logger           *slog.Logger
}

// NewDetector creates a new Detector
func NewDetector(device string, baudRates []int, detectionTimeout time.Duration, minBytesForValid int, logger *slog.Logger) *Detector {
	return &Detector{
		device:           device,
		baudRates:        baudRates,
		detectionTimeout: detectionTimeout,
		minBytesForValid: minBytesForValid,
		logger:           logger,
	}
}

// DetectBaudRate attempts to detect the correct baud rate
// Returns the detected baud rate or an error
func (d *Detector) DetectBaudRate() (int, error) {
	d.logger.Info("Starting autobaud detection", "device", d.device, "rates", d.baudRates)

	for i, baudRate := range d.baudRates {
		// Add settling delay between attempts (not before first)
		// This allows USB-to-serial adapters to stabilize after close
		if i > 0 {
			time.Sleep(DetectionSettlingTime)
		}

		d.logger.Debug("Trying baud rate", "device", d.device, "baud", baudRate)

		reader, err := NewRealReader(d.device, baudRate, false)
		if err != nil {
			d.logger.Warn("Failed to open port", "device", d.device, "baud", baudRate, "error", err)
			continue
		}

		// Flush any stale data from previous baud rate test
		// This prevents contamination of ASCII ratio calculation
		if err := reader.ResetInputBuffer(); err != nil {
			d.logger.Debug("Failed to reset input buffer", "device", d.device, "error", err)
			// Non-fatal - continue with detection
		}

		validityRatio, bytesRead := d.testBaudRate(reader)
		reader.Close()

		d.logger.Debug("Baud rate test result",
			"device", d.device,
			"baud", baudRate,
			"validity", fmt.Sprintf("%.2f", validityRatio),
			"bytes", bytesRead)

		// Success criteria: validity ratio >= threshold AND enough bytes read
		if validityRatio >= ValidityThreshold && bytesRead >= d.minBytesForValid {
			d.logger.Info("Detected baud rate",
				"device", d.device,
				"baud", baudRate,
				"validity", fmt.Sprintf("%.2f", validityRatio),
				"bytes", bytesRead)
			return baudRate, nil
		}
	}

	return 0, fmt.Errorf("failed to detect baud rate for %s after trying all rates", d.device)
}

// DetectPinout attempts to detect the correct pinout (flow control settings)
// Returns true if flow control should be used
func (d *Detector) DetectPinout(baudRate int) (bool, error) {
	d.logger.Info("Starting pinout detection", "device", d.device, "baud", baudRate)

	// Try with flow control first (straight-through cable)
	d.logger.Debug("Testing with flow control enabled", "device", d.device)
	if success := d.testFlowControl(baudRate, true); success {
		d.logger.Info("Detected straight-through cable (flow control enabled)", "device", d.device)
		return true, nil
	}

	// Try without flow control (null modem cable)
	d.logger.Debug("Testing with flow control disabled", "device", d.device)
	if success := d.testFlowControl(baudRate, false); success {
		d.logger.Info("Detected null modem cable (flow control disabled)", "device", d.device)
		return false, nil
	}

	return false, fmt.Errorf("failed to detect pinout for %s - no data received", d.device)
}

// testBaudRate tests a specific baud rate and returns validity ratio and bytes read
func (d *Detector) testBaudRate(reader Reader) (float64, int) {
	buf := make([]byte, DetectionBufferSize)
	totalBytes := 0
	validChars := 0
	deadline := time.Now().Add(d.detectionTimeout)

	for time.Now().Before(deadline) {
		n, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			// Read error, stop testing
			break
		}

		if n > 0 {
			totalBytes += n
			validChars += countValidASCII(buf[:n])

			// If we have enough data, we can stop early
			if totalBytes >= d.minBytesForValid {
				break
			}
		}

		// Brief pause to avoid busy loop
		time.Sleep(DetectionPollInterval)
	}

	if totalBytes == 0 {
		return 0.0, 0
	}

	validityRatio := float64(validChars) / float64(totalBytes)
	return validityRatio, totalBytes
}

// testFlowControl tests if data can be received with the given flow control setting
func (d *Detector) testFlowControl(baudRate int, useFlowControl bool) bool {
	reader, err := NewRealReader(d.device, baudRate, useFlowControl)
	if err != nil {
		d.logger.Warn("Failed to open port for pinout test",
			"device", d.device,
			"flow_control", useFlowControl,
			"error", err)
		return false
	}
	defer reader.Close()

	buf := make([]byte, FlowControlTestBufferSize)
	deadline := time.Now().Add(d.detectionTimeout)

	for time.Now().Before(deadline) {
		n, err := reader.Read(buf)
		if err != nil && err != io.EOF {
			// Read error
			return false
		}

		if n > 0 {
			// Got data, success!
			validChars := countValidASCII(buf[:n])
			validityRatio := float64(validChars) / float64(n)

			// Must be mostly valid ASCII
			if validityRatio >= ValidityThreshold {
				return true
			}
		}

		// Brief pause
		time.Sleep(DetectionPollInterval)
	}

	return false
}

// countValidASCII counts printable ASCII and common control characters.
// Used during autobaud detection - at correct baud rate, text data is ~95%+
// printable ASCII. At wrong baud rate, random bit patterns yield ~35-50%.
func countValidASCII(data []byte) int {
	count := 0
	for _, b := range data {
		// Printable ASCII (space through tilde) + TAB, CR, LF
		if (b >= 0x20 && b <= 0x7E) || b == 0x09 || b == 0x0A || b == 0x0D {
			count++
		}
	}
	return count
}

// Detect runs full detection (baud rate only, no pinout)
func (d *Detector) Detect() (*DetectionResult, error) {
	// Detect baud rate only
	baudRate, err := d.DetectBaudRate()
	if err != nil {
		return nil, err
	}

	// Always use no flow control (null modem default)
	return &DetectionResult{
		BaudRate:       baudRate,
		UseFlowControl: false,
	}, nil
}
