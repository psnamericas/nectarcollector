package capture

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"nectarcollector/config"
	"nectarcollector/output"
)

// MaxHTTPBodySize is the maximum size of an HTTP POST body (50MB)
const MaxHTTPBodySize = 50 * 1024 * 1024

// HTTPChannel handles CDR capture from HTTP POST requests
type HTTPChannel struct {
	config    config.PortConfig
	appConfig config.AppConfig
	logger    *slog.Logger

	dualWriter *output.DualWriter

	// Stats
	statsMutex   sync.RWMutex
	stats        HTTPChannelStats
	bytesRead    atomic.Int64
	requestCount atomic.Int64
	errorCount   atomic.Int64
}

// HTTPChannelStats tracks statistics for an HTTP capture channel
type HTTPChannelStats struct {
	BytesRead       int64     `json:"bytes_read"`
	RequestCount    int64     `json:"requests"`
	Errors          int64     `json:"errors"`
	LastRequestTime time.Time `json:"last_request_time"`
	StartTime       time.Time `json:"start_time"`
}

// NewHTTPChannel creates a new HTTP capture channel
func NewHTTPChannel(
	portCfg config.PortConfig,
	appCfg config.AppConfig,
	dualWriter *output.DualWriter,
	logger *slog.Logger,
) *HTTPChannel {
	return &HTTPChannel{
		config:     portCfg,
		appConfig:  appCfg,
		dualWriter: dualWriter,
		logger:     logger.With("channel", portCfg.SideDesignation, "path", portCfg.Path),
		stats: HTTPChannelStats{
			StartTime: time.Now(),
		},
	}
}

// ServeHTTP handles incoming HTTP POST requests
func (h *HTTPChannel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Only accept POST
	if r.Method != http.MethodPost {
		h.errorCount.Add(1)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Limit body size
	r.Body = http.MaxBytesReader(w, r.Body, MaxHTTPBodySize)

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		h.errorCount.Add(1)
		h.logger.Warn("Failed to read request body", "error", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	if len(body) == 0 {
		h.errorCount.Add(1)
		http.Error(w, "Empty body", http.StatusBadRequest)
		return
	}

	// Build the record with headers
	record := h.buildRecord(r, body)

	// Get FIPS code
	fipsCode := h.config.FIPSCode
	if fipsCode == "" {
		fipsCode = h.appConfig.FIPSCode
	}

	// Build header and write
	header := output.BuildHeader(fipsCode, h.config.SideDesignation, time.Now().UTC())
	fullRecord := header + record

	if err := h.dualWriter.WriteLine(fullRecord); err != nil {
		h.errorCount.Add(1)
		h.logger.Warn("Failed to write record", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Update stats
	h.bytesRead.Add(int64(len(body)))
	h.requestCount.Add(1)
	h.statsMutex.Lock()
	h.stats.LastRequestTime = time.Now()
	h.statsMutex.Unlock()

	h.logger.Debug("Captured HTTP POST",
		"content_length", len(body),
		"content_type", r.Header.Get("Content-Type"))

	// Success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// buildRecord constructs the full record with headers and body
func (h *HTTPChannel) buildRecord(r *http.Request, body []byte) string {
	var record string

	// Add request line
	record += fmt.Sprintf("%s %s %s\n", r.Method, r.URL.RequestURI(), r.Proto)

	// Add headers
	for name, values := range r.Header {
		for _, value := range values {
			record += fmt.Sprintf("%s: %s\n", name, value)
		}
	}

	// Add remote addr as custom header
	record += fmt.Sprintf("X-Remote-Addr: %s\n", r.RemoteAddr)

	// Blank line separating headers from body
	record += "\n"

	// Add body
	record += string(body)

	return record
}

// GetStats returns current channel statistics
func (h *HTTPChannel) GetStats() HTTPChannelStats {
	h.statsMutex.RLock()
	defer h.statsMutex.RUnlock()

	return HTTPChannelStats{
		BytesRead:       h.bytesRead.Load(),
		RequestCount:    h.requestCount.Load(),
		Errors:          h.errorCount.Load(),
		LastRequestTime: h.stats.LastRequestTime,
		StartTime:       h.stats.StartTime,
	}
}

// Config returns the port configuration
func (h *HTTPChannel) Config() config.PortConfig {
	return h.config
}

// Path returns the HTTP path this channel handles
func (h *HTTPChannel) Path() string {
	return h.config.Path
}

// SideDesignation returns the A designation for this channel
func (h *HTTPChannel) SideDesignation() string {
	return h.config.SideDesignation
}

// Stop closes the HTTP channel's dual writer
func (h *HTTPChannel) Stop() error {
	h.logger.Info("Stopping HTTP channel", "path", h.config.Path)
	if h.dualWriter != nil {
		return h.dualWriter.Close()
	}
	return nil
}
