package serial

import (
	"fmt"
	"io"
	"sync"
	"time"

	"go.bug.st/serial"
)

// Reader interface for serial port reading
type Reader interface {
	io.Reader
	io.Closer
	Device() string
	IsOpen() bool
	Reconfigure(baudRate int, useFlowControl bool) error
}

// RealReader implements Reader using go.bug.st/serial
type RealReader struct {
	device         string
	port           serial.Port
	baudRate       int
	useFlowControl bool
	isOpen         bool
	mu             sync.Mutex
}

// NewRealReader creates a new RealReader
func NewRealReader(device string, baudRate int, useFlowControl bool) (*RealReader, error) {
	reader := &RealReader{
		device:         device,
		baudRate:       baudRate,
		useFlowControl: useFlowControl,
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

	mode := &serial.Mode{
		BaudRate: r.baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(r.device, mode)
	if err != nil {
		return fmt.Errorf("failed to open %s: %w", r.device, err)
	}

	// Set read timeout
	if err := port.SetReadTimeout(5 * time.Second); err != nil {
		port.Close()
		return fmt.Errorf("failed to set read timeout: %w", err)
	}

	// Configure flow control
	if r.useFlowControl {
		// Enable RTS/CTS hardware flow control
		if err := port.SetMode(mode); err != nil {
			port.Close()
			return fmt.Errorf("failed to set flow control: %w", err)
		}
	}

	r.port = port
	r.isOpen = true

	return nil
}

// Read implements io.Reader
func (r *RealReader) Read(p []byte) (n int, err error) {
	r.mu.Lock()
	port := r.port
	r.mu.Unlock()

	if port == nil {
		return 0, fmt.Errorf("port not open")
	}

	return port.Read(p)
}

// Close implements io.Closer
func (r *RealReader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.isOpen || r.port == nil {
		return nil
	}

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
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isOpen
}

// Reconfigure closes and reopens the port with new settings
func (r *RealReader) Reconfigure(baudRate int, useFlowControl bool) error {
	if err := r.Close(); err != nil {
		return fmt.Errorf("failed to close port: %w", err)
	}

	r.baudRate = baudRate
	r.useFlowControl = useFlowControl

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
