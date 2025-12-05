package serial

import (
	"fmt"
	"io"
	"sync"
	"time"

	"go.bug.st/serial"
)

// Serial port timing constants
const (
	// DefaultReadTimeout is the timeout for production reads.
	// Shorter timeouts allow faster shutdown response while still being
	// efficient for data collection. The scanner will retry on timeout.
	DefaultReadTimeout = 500 * time.Millisecond

	// DetectionReadTimeout is longer to allow baud rate detection to
	// accumulate enough data for accurate ASCII ratio calculation.
	DetectionReadTimeout = 5 * time.Second
)

// ModemStatus represents the state of modem control lines
type ModemStatus struct {
	CTS bool // Clear To Send
	DSR bool // Data Set Ready
	DCD bool // Data Carrier Detect (also called CD or RLSD)
	RI  bool // Ring Indicator
}

// Reader interface for serial port reading
type Reader interface {
	io.Reader
	io.Closer
	Device() string
	IsOpen() bool
	Reconfigure(baudRate int, useFlowControl bool) error
	SetReadTimeout(timeout time.Duration) error
	ResetInputBuffer() error
	GetModemStatus() (*ModemStatus, error)
}

// SerialConfig holds all serial port configuration parameters
type SerialConfig struct {
	BaudRate       int
	DataBits       int    // 5, 6, 7, or 8
	Parity         string // "none", "odd", "even", "mark", "space"
	StopBits       int    // 1 or 2
	UseFlowControl bool
}

// DefaultSerialConfig returns the standard 8N1 configuration
func DefaultSerialConfig(baudRate int, useFlowControl bool) SerialConfig {
	return SerialConfig{
		BaudRate:       baudRate,
		DataBits:       8,
		Parity:         "none",
		StopBits:       1,
		UseFlowControl: useFlowControl,
	}
}

// parityFromString converts a parity string to go.bug.st/serial Parity type
func parityFromString(p string) serial.Parity {
	switch p {
	case "odd":
		return serial.OddParity
	case "even":
		return serial.EvenParity
	case "mark":
		return serial.MarkParity
	case "space":
		return serial.SpaceParity
	default:
		return serial.NoParity
	}
}

// stopBitsFromInt converts stop bits int to go.bug.st/serial StopBits type
func stopBitsFromInt(s int) serial.StopBits {
	if s == 2 {
		return serial.TwoStopBits
	}
	return serial.OneStopBit
}

// RealReader implements Reader using go.bug.st/serial
type RealReader struct {
	device string
	port   serial.Port
	config SerialConfig
	isOpen bool
	mu     sync.RWMutex // RWMutex allows concurrent reads while blocking on close
}

// NewRealReader creates a new RealReader with basic 8N1 configuration
// For full configuration options, use NewRealReaderWithConfig
func NewRealReader(device string, baudRate int, useFlowControl bool) (*RealReader, error) {
	return NewRealReaderWithConfig(device, DefaultSerialConfig(baudRate, useFlowControl))
}

// NewRealReaderWithConfig creates a new RealReader with full serial configuration
func NewRealReaderWithConfig(device string, config SerialConfig) (*RealReader, error) {
	reader := &RealReader{
		device: device,
		config: config,
	}

	if err := reader.open(); err != nil {
		return nil, err
	}

	return reader, nil
}

// open opens the serial port with current configuration
func (r *RealReader) open() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.isOpen {
		return fmt.Errorf("port already open")
	}

	// Apply defaults for zero values
	dataBits := r.config.DataBits
	if dataBits == 0 {
		dataBits = 8
	}

	mode := &serial.Mode{
		BaudRate: r.config.BaudRate,
		DataBits: dataBits,
		Parity:   parityFromString(r.config.Parity),
		StopBits: stopBitsFromInt(r.config.StopBits),
	}

	port, err := serial.Open(r.device, mode)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", r.device, err)
	}

	// Set read timeout - use detection timeout initially, can be changed later
	// for production reads via SetReadTimeout()
	if err := port.SetReadTimeout(DetectionReadTimeout); err != nil {
		port.Close()
		return fmt.Errorf("failed to set read timeout: %w", err)
	}

	// Configure modem control signals
	// For receive-only capture, we assert RTS and DTR to signal we're ready
	// This is critical for devices that use hardware flow control
	if r.config.UseFlowControl {
		// Assert RTS (Request To Send) - tells sender we're ready to receive
		if err := port.SetRTS(true); err != nil {
			port.Close()
			return fmt.Errorf("failed to set RTS: %w", err)
		}
		// Assert DTR (Data Terminal Ready) - tells DCE we're online
		if err := port.SetDTR(true); err != nil {
			port.Close()
			return fmt.Errorf("failed to set DTR: %w", err)
		}
	} else {
		// Even without flow control, some devices need DTR asserted to send data
		// This mimics Scannex behavior - always ready to receive
		if err := port.SetDTR(true); err != nil {
			// Non-fatal - some ports don't support DTR control
		}
	}

	r.port = port
	r.isOpen = true

	return nil
}

// Read implements io.Reader
// Uses RLock to allow concurrent reads while blocking Close() until all reads complete
func (r *RealReader) Read(p []byte) (n int, err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.isOpen || r.port == nil {
		return 0, fmt.Errorf("port not open")
	}

	return r.port.Read(p)
}

// Close implements io.Closer
// Uses full Lock to wait for all concurrent reads to complete before closing
func (r *RealReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.isOpen || r.port == nil {
		return nil
	}

	// Drain any pending output data before closing (best practice since RS-232 days)
	// This ensures we don't lose data in transit
	if err := r.port.Drain(); err != nil {
		// Log but don't fail - drain errors are common on already-disconnected ports
		// The port may already be gone (USB unplug, etc.)
	}

	// Clear input buffer to prevent stale data on reconnect
	_ = r.port.ResetInputBuffer()

	err := r.port.Close()
	r.port = nil
	r.isOpen = false

	return err
}

// Device returns the device path
func (r *RealReader) Device() string {
	return r.device
}

// IsOpen returns true if the port is open
func (r *RealReader) IsOpen() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.isOpen
}

// SetReadTimeout sets the read timeout for the serial port.
// Use DefaultReadTimeout for production reads (faster shutdown response)
// or DetectionReadTimeout for baud rate detection (needs more data).
func (r *RealReader) SetReadTimeout(timeout time.Duration) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.isOpen || r.port == nil {
		return fmt.Errorf("port not open")
	}

	return r.port.SetReadTimeout(timeout)
}

// ResetInputBuffer clears any pending input data from the serial port buffer.
// This is critical during baud rate detection to avoid contamination from
// data received at the wrong baud rate.
func (r *RealReader) ResetInputBuffer() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.isOpen || r.port == nil {
		return fmt.Errorf("port not open")
	}

	return r.port.ResetInputBuffer()
}

// GetModemStatus returns the current state of modem control lines.
// This can be used to detect cable disconnections (DCD/DSR going low)
// similar to how Scannex boxes monitor RS232 signal levels.
func (r *RealReader) GetModemStatus() (*ModemStatus, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if !r.isOpen || r.port == nil {
		return nil, fmt.Errorf("port not open")
	}

	bits, err := r.port.GetModemStatusBits()
	if err != nil {
		return nil, fmt.Errorf("failed to get modem status: %w", err)
	}

	return &ModemStatus{
		CTS: bits.CTS,
		DSR: bits.DSR,
		DCD: bits.DCD,
		RI:  bits.RI,
	}, nil
}

// Reconfigure closes and reopens the port with new settings.
// This is atomic - the port is either fully reconfigured or left closed on error.
func (r *RealReader) Reconfigure(baudRate int, useFlowControl bool) error {
	// Close first (this acquires its own lock)
	if err := r.Close(); err != nil {
		return fmt.Errorf("failed to close port: %w", err)
	}

	// Now update settings under lock before reopening
	r.mu.Lock()
	r.config.BaudRate = baudRate
	r.config.UseFlowControl = useFlowControl
	r.mu.Unlock()

	// open() acquires its own lock internally
	return r.open()
}

// ReaderWithStats wraps a Reader to track statistics
type ReaderWithStats struct {
	reader    Reader
	bytesRead int64
	linesRead int64
	errors    int64
	mu        sync.RWMutex
}

// NewReaderWithStats creates a new ReaderWithStats
func NewReaderWithStats(reader Reader) *ReaderWithStats {
	return &ReaderWithStats{
		reader: reader,
	}
}

// Read implements io.Reader and tracks bytes read
func (r *ReaderWithStats) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)

	r.mu.Lock()
	r.bytesRead += int64(n)
	if err != nil && err != io.EOF {
		r.errors++
	}
	r.mu.Unlock()

	return n, err
}

// Close implements io.Closer
func (r *ReaderWithStats) Close() error {
	return r.reader.Close()
}

// Device returns the device path
func (r *ReaderWithStats) Device() string {
	return r.reader.Device()
}

// IsOpen returns true if the port is open
func (r *ReaderWithStats) IsOpen() bool {
	return r.reader.IsOpen()
}

// Reconfigure reconfigures the underlying reader
func (r *ReaderWithStats) Reconfigure(baudRate int, useFlowControl bool) error {
	return r.reader.Reconfigure(baudRate, useFlowControl)
}

// SetReadTimeout sets the read timeout on the underlying reader
func (r *ReaderWithStats) SetReadTimeout(timeout time.Duration) error {
	return r.reader.SetReadTimeout(timeout)
}

// ResetInputBuffer clears the input buffer on the underlying reader
func (r *ReaderWithStats) ResetInputBuffer() error {
	return r.reader.ResetInputBuffer()
}

// GetModemStatus returns the modem status from the underlying reader
func (r *ReaderWithStats) GetModemStatus() (*ModemStatus, error) {
	return r.reader.GetModemStatus()
}

// LineRead increments the line counter
func (r *ReaderWithStats) LineRead() {
	r.mu.Lock()
	r.linesRead++
	r.mu.Unlock()
}

// IncrementErrors increments the error counter
func (r *ReaderWithStats) IncrementErrors() {
	r.mu.Lock()
	r.errors++
	r.mu.Unlock()
}

// Stats returns current statistics
func (r *ReaderWithStats) Stats() (bytesRead, linesRead, errors int64) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.bytesRead, r.linesRead, r.errors
}

// ResetStats resets all statistics
func (r *ReaderWithStats) ResetStats() {
	r.mu.Lock()
	r.bytesRead = 0
	r.linesRead = 0
	r.errors = 0
	r.mu.Unlock()
}
