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
	config      *config.MonitoringConfig
	manager     *capture.Manager
	logger      *slog.Logger
	server      *http.Server
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

	// Wrap with basic auth if configured
	var handler http.Handler = mux
	if s.config.Username != "" && s.config.Password != "" {
		handler = s.basicAuth(mux)
		s.logger.Info("Basic auth enabled for HoneyView")
	}

	addr := fmt.Sprintf(":%d", s.config.Port)
	s.server = &http.Server{
		Addr:    addr,
		Handler: handler,
	}

	s.logger.Info("Starting HoneyView monitoring server", "port", s.config.Port)

	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HoneyView server error", "error", err)
		}
	}()

	return nil
}

// basicAuth wraps a handler with HTTP Basic Authentication
func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.config.Username || pass != s.config.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="NectarCollector"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
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

// handleFeed returns the last N lines from a channel's log file (tail)
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	channel := r.URL.Query().Get("channel")
	if channel == "" {
		http.Error(w, "channel parameter required", http.StatusBadRequest)
		return
	}

	// Parse optional count parameter (default 50, max 200)
	count := 50
	if countStr := r.URL.Query().Get("count"); countStr != "" {
		if n, err := strconv.Atoi(countStr); err == nil && n > 0 {
			count = n
		}
	}
	if count > 200 {
		count = 200
	}

	logPath := filepath.Join(s.logBasePath, channel+".log")
	lines, err := tailFile(logPath, count)
	if err != nil {
		s.logger.Warn("Failed to read log file", "path", logPath, "error", err)
		lines = []string{}
	}

	response := map[string]interface{}{
		"channel": channel,
		"lines":   lines,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// tailFile returns the last n lines from a file.
// Uses a ring buffer to keep memory bounded regardless of file size.
func tailFile(path string, n int) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Ring buffer to hold last n lines
	ring := make([]string, n)
	idx := 0
	count := 0

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		ring[idx] = scanner.Text()
		idx = (idx + 1) % n
		count++
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Extract lines in correct order
	if count == 0 {
		return []string{}, nil
	}

	if count < n {
		// File has fewer lines than requested
		return ring[:count], nil
	}

	// Reorder ring buffer: idx points to oldest line
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = ring[(idx+i)%n]
	}
	return result, nil
}
