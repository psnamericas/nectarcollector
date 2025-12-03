package monitoring

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"nectarcollector/capture"
	"nectarcollector/config"
)

//go:embed dashboard.html
var dashboardHTML embed.FS

// Server provides HTTP monitoring endpoints
type Server struct {
	config     *config.MonitoringConfig
	manager    *capture.Manager
	logger     *slog.Logger
	server     *http.Server
	logBasePath string
}

// NewServer creates a new monitoring server
func NewServer(cfg *config.MonitoringConfig, manager *capture.Manager, logBasePath string, logger *slog.Logger) *Server {
	return &Server{
		config:      cfg,
		manager:     manager,
		logBasePath: logBasePath,
		logger:      logger,
	}
}

// Start starts the monitoring HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Dashboard
	mux.HandleFunc("/", s.handleDashboard)

	// API endpoints
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/feed", s.handleFeed)

	addr := fmt.Sprintf(":%d", s.config.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	s.logger.Info("Starting HoneyView monitoring server", "port", s.config.Port)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HoneyView server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully stops the monitoring server
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		s.logger.Info("Stopping HoneyView monitoring server")
		return s.server.Shutdown(ctx)
	}
	return nil
}

// handleDashboard serves the HoneyView dashboard
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	data, err := dashboardHTML.ReadFile("dashboard.html")
	if err != nil {
		http.Error(w, "Dashboard not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write(data)
}

// handleHealth returns health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(health)
}

// handleStats returns channel statistics
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	stats := s.manager.GetAllStats()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleFeed returns recent lines from a channel's log file
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	channel := r.URL.Query().Get("channel")
	offsetStr := r.URL.Query().Get("offset")

	if channel == "" {
		http.Error(w, "channel parameter required", http.StatusBadRequest)
		return
	}

	// Parse offset (line number to start from)
	offset := 0
	if offsetStr != "" {
		if off, err := strconv.Atoi(offsetStr); err == nil {
			offset = off
		}
	}

	// Construct log file path: /var/log/nectarcollector/1429010002-A5.log
	logPath := filepath.Join(s.logBasePath, channel+".log")

	// Read lines starting from offset
	lines, totalLines, err := s.readLinesFromOffset(logPath, offset)
	if err != nil {
		s.logger.Warn("Failed to read log file", "path", logPath, "error", err)
		lines = []string{} // Return empty array on error
		totalLines = offset
	}

	response := map[string]interface{}{
		"channel":     channel,
		"lines":       lines,
		"total_lines": totalLines,
		"offset":      offset,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// readLinesFromOffset reads lines from a log file starting from a given line offset
// Returns the lines read, total line count in file, and any error
func (s *Server) readLinesFromOffset(logPath string, offset int) ([]string, int, error) {
	file, err := os.Open(logPath)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	var allLines []string
	scanner := bufio.NewScanner(file)

	// Read all lines (we need total count anyway)
	for scanner.Scan() {
		line := scanner.Text()
		allLines = append(allLines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	totalLines := len(allLines)

	// If offset is beyond current lines, return empty (nothing new)
	if offset >= totalLines {
		return []string{}, totalLines, nil
	}

	// Return lines from offset onwards (new lines only)
	newLines := allLines[offset:]

	// Limit to 100 new lines per request to avoid overwhelming browser
	if len(newLines) > 100 {
		newLines = newLines[:100]
	}

	return newLines, totalLines, nil
}
