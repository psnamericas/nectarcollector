package monitoring

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"nectarcollector/capture"
	"nectarcollector/config"
	"nectarcollector/serial"

	"github.com/nats-io/nats.go"
)

// getInode extracts the inode number from file info (Unix only)
func getInode(info os.FileInfo) (uint64, bool) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ino, true
	}
	return 0, false
}

//go:embed dashboard.html
var dashboardHTML embed.FS

//go:embed logix.png
var logixLogo []byte

// SSEClient represents a connected SSE client
type SSEClient struct {
	channel string
	send    chan string
	done    chan struct{}
}

// SSEBroker manages SSE client connections and message broadcasting
type SSEBroker struct {
	clients    map[*SSEClient]bool
	register   chan *SSEClient
	unregister chan *SSEClient
	broadcast  chan BroadcastMessage
	mu         sync.RWMutex
}

// BroadcastMessage contains a line and its target channel
type BroadcastMessage struct {
	Channel string
	Line    string
}

// NewSSEBroker creates a new SSE broker
func NewSSEBroker() *SSEBroker {
	return &SSEBroker{
		clients:    make(map[*SSEClient]bool),
		register:   make(chan *SSEClient),
		unregister: make(chan *SSEClient),
		broadcast:  make(chan BroadcastMessage, 256),
	}
}

// Run starts the broker's main loop
func (b *SSEBroker) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			// Close all client connections
			b.mu.Lock()
			for client := range b.clients {
				close(client.done)
				delete(b.clients, client)
			}
			b.mu.Unlock()
			return

		case client := <-b.register:
			b.mu.Lock()
			b.clients[client] = true
			b.mu.Unlock()

		case client := <-b.unregister:
			b.mu.Lock()
			if _, ok := b.clients[client]; ok {
				close(client.done)
				delete(b.clients, client)
			}
			b.mu.Unlock()

		case msg := <-b.broadcast:
			b.mu.RLock()
			for client := range b.clients {
				// Send to clients subscribed to this channel or "all"
				if client.channel == msg.Channel || client.channel == "all" {
					select {
					case client.send <- msg.Line:
					default:
						// Client buffer full, skip this message
					}
				}
			}
			b.mu.RUnlock()
		}
	}
}

// Broadcast sends a line to all clients subscribed to the channel
func (b *SSEBroker) Broadcast(channel, line string) {
	select {
	case b.broadcast <- BroadcastMessage{Channel: channel, Line: line}:
	default:
		// Broadcast buffer full, drop message
	}
}

// ClientCount returns the number of connected clients
func (b *SSEBroker) ClientCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.clients)
}

// Server provides HTTP monitoring endpoints
type Server struct {
	config      *config.MonitoringConfig
	manager     *capture.Manager
	logger      *slog.Logger
	server      *http.Server
	httpServers []*http.Server // Additional servers for HTTP capture on custom ports
	logBasePath string
	broker      *SSEBroker
	version     string
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewServer creates a new monitoring server
func NewServer(cfg *config.MonitoringConfig, manager *capture.Manager, logBasePath string, logger *slog.Logger, version string) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	broker := NewSSEBroker()

	s := &Server{
		config:      cfg,
		manager:     manager,
		logBasePath: logBasePath,
		logger:      logger,
		broker:      broker,
		version:     version,
		ctx:         ctx,
		cancel:      cancel,
	}

	// Start broker
	go broker.Run(ctx)

	// Start log watchers for each channel
	go s.watchLogFiles(ctx)

	return s
}

// watchLogFiles monitors log files and broadcasts new lines
func (s *Server) watchLogFiles(ctx context.Context) {
	// Wait for channels to be available
	time.Sleep(2 * time.Second)

	channels := s.manager.GetChannels()
	for _, ch := range channels {
		go s.tailLogFile(ctx, ch)
	}
}

// tailLogFile tails a log file and broadcasts new lines
func (s *Server) tailLogFile(ctx context.Context, ch *capture.Channel) {
	// Get identifier from channel - matches the format used in channel.go
	identifier := fmt.Sprintf("%s-%s", ch.FIPSCode(), ch.SideDesignation())

	logPath := filepath.Join(s.logBasePath, identifier+".log")

	s.logger.Debug("Starting log tail", "channel", identifier, "path", logPath)

	var lastInode uint64
	var currentPos int64

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Check file info to detect rotation
		info, err := os.Stat(logPath)
		if err != nil {
			// File doesn't exist yet, wait and retry
			time.Sleep(1 * time.Second)
			continue
		}

		// Get inode to detect file rotation
		stat, ok := getInode(info)
		if ok && lastInode != 0 && stat != lastInode {
			// File was rotated, reset position
			s.logger.Debug("Log file rotated", "channel", identifier)
			currentPos = 0
		}
		lastInode = stat

		// Open file
		file, err := os.Open(logPath)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		// Seek to last known position (or end if first time)
		if currentPos == 0 {
			currentPos, _ = file.Seek(0, 2) // Seek to end
		} else {
			file.Seek(currentPos, 0) // Seek to saved position
		}

		reader := bufio.NewReader(file)
		linesRead := 0

		for {
			select {
			case <-ctx.Done():
				file.Close()
				return
			default:
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				// Save current position and close file to allow rotation
				currentPos, _ = file.Seek(0, 1)
				file.Close()
				// Wait before reopening
				time.Sleep(100 * time.Millisecond)
				break // Break inner loop to reopen file
			}

			linesRead++

			// Remove trailing newline
			if len(line) > 0 && line[len(line)-1] == '\n' {
				line = line[:len(line)-1]
			}
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}

			if line != "" {
				s.broker.Broadcast(identifier, line)
			}
		}
	}
}

// Start starts the monitoring HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Dashboard
	mux.HandleFunc("/", s.handleDashboard)
	mux.HandleFunc("/media/logix.png", s.handleLogo)

	// API endpoints
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/ports", s.handlePorts)
	mux.HandleFunc("/api/ports/config", s.handlePortsConfig)
	mux.HandleFunc("/api/ports/config/", s.handlePortConfigAction)
	mux.HandleFunc("/api/ports/available", s.handleAvailablePorts)
	mux.HandleFunc("/api/system", s.handleSystem)
	mux.HandleFunc("/api/feed", s.handleFeed)
	mux.HandleFunc("/api/stream", s.handleSSE)
	mux.HandleFunc("/api/events", s.handleEvents)

	// Group HTTP channels by listen port
	httpChannels := s.manager.GetHTTPChannels()
	mainPortChannels := make([]*capture.HTTPChannel, 0)
	customPortChannels := make(map[int][]*capture.HTTPChannel)

	for _, ch := range httpChannels {
		cfg := ch.Config()
		if cfg.ListenPort == 0 || cfg.ListenPort == s.config.Port {
			// Use main monitoring port
			mainPortChannels = append(mainPortChannels, ch)
		} else {
			// Custom port
			customPortChannels[cfg.ListenPort] = append(customPortChannels[cfg.ListenPort], ch)
		}
	}

	// Register channels on main port
	for _, ch := range mainPortChannels {
		path := ch.Path()
		s.logger.Info("Registering HTTP capture endpoint",
			"path", path,
			"port", s.config.Port,
			"designation", ch.SideDesignation())
		mux.Handle(path, ch)
	}

	// Create handler that applies auth selectively
	var handler http.Handler
	if s.config.Username != "" && s.config.Password != "" {
		// Apply auth to everything except HTTP capture endpoints
		handler = s.selectiveAuth(mux, mainPortChannels)
		s.logger.Info("Basic auth enabled for HoneyView (CDR endpoints excluded)")
	} else {
		handler = mux
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

	// Start separate servers for HTTP channels with custom ports
	for port, channels := range customPortChannels {
		if err := s.startHTTPCaptureServer(port, channels); err != nil {
			s.logger.Error("Failed to start HTTP capture server", "port", port, "error", err)
			// Continue with other ports - don't fail entirely
		}
	}

	return nil
}

// startHTTPCaptureServer starts a dedicated HTTP server for capture endpoints on a custom port
func (s *Server) startHTTPCaptureServer(port int, channels []*capture.HTTPChannel) error {
	mux := http.NewServeMux()

	for _, ch := range channels {
		path := ch.Path()
		s.logger.Info("Registering HTTP capture endpoint",
			"path", path,
			"port", port,
			"designation", ch.SideDesignation())
		mux.Handle(path, ch)
	}

	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	s.httpServers = append(s.httpServers, server)

	s.logger.Info("Starting HTTP capture server", "port", port, "endpoints", len(channels))

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Error("HTTP capture server error", "port", port, "error", err)
		}
	}()

	return nil
}

// selectiveAuth applies basic auth except for CDR ingestion endpoints
func (s *Server) selectiveAuth(next http.Handler, httpChannels []*capture.HTTPChannel) http.Handler {
	// Build set of paths that don't need auth
	noAuthPaths := make(map[string]bool)
	for _, ch := range httpChannels {
		noAuthPaths[ch.Path()] = true
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for CDR ingestion endpoints
		if noAuthPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		// Apply basic auth for everything else
		user, pass, ok := r.BasicAuth()
		if !ok || user != s.config.Username || pass != s.config.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="HoneyView"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
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
	// Cancel broker and watchers first - this closes SSE client connections
	s.cancel()

	// Use a shorter timeout for HTTP shutdown (5 seconds max)
	// SSE connections should close quickly once broker signals them
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var lastErr error

	// Shutdown additional HTTP capture servers
	for _, server := range s.httpServers {
		s.logger.Info("Stopping HTTP capture server", "addr", server.Addr)
		if err := server.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("Error stopping HTTP capture server", "addr", server.Addr, "error", err)
			lastErr = err
		}
	}

	// Shutdown main monitoring server
	if s.server != nil {
		s.logger.Info("Stopping HoneyView monitoring server")
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			lastErr = err
		}
	}

	return lastErr
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

// handleLogo serves the 911 Logix logo
func (s *Server) handleLogo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400") // Cache for 1 day
	w.Write(logixLogo)
}

// handleHealth returns health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":      "healthy",
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"sse_clients": s.broker.ClientCount(),
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

// PortStatus represents the status of a single COM port
type PortStatus struct {
	Device    string `json:"device"`
	COM       string `json:"com"`
	Connected bool   `json:"connected"`
	CTS       bool   `json:"cts"`
	DSR       bool   `json:"dsr"`
	DCD       bool   `json:"dcd"`
	RI        bool   `json:"ri"`
	InUse     string `json:"in_use,omitempty"` // Channel using this port, if any
}

// handlePorts returns status of all available COM ports
func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	// Build map of configured ports to channels
	channelsByDevice := make(map[string]*capture.Channel)
	for _, ch := range s.manager.GetChannels() {
		channelsByDevice[ch.Device()] = ch
	}

	// Scan standard COM ports (ttyS1-ttyS5, skipping ttyS0 which is console)
	ports := []PortStatus{}
	comNames := map[string]string{
		"/dev/ttyS1": "COM2",
		"/dev/ttyS2": "COM3",
		"/dev/ttyS3": "COM4",
		"/dev/ttyS4": "COM5",
		"/dev/ttyS5": "COM6",
	}

	for device, com := range comNames {
		status := PortStatus{
			Device: device,
			COM:    com,
		}

		// Check if this port is in use by a channel
		if ch, ok := channelsByDevice[device]; ok {
			status.InUse = ch.SideDesignation()
			// Get signals from the active channel's stats
			stats := ch.Stats()
			if stats.Signals != nil {
				status.CTS = stats.Signals.CTS
				status.DSR = stats.Signals.DSR
				status.DCD = stats.Signals.DCD
				status.RI = stats.Signals.RI
				status.Connected = stats.Signals.DCD || stats.Signals.DSR
			}
		} else {
			// Port not in use - try to read modem signals directly
			if reader, err := serial.NewRealReader(device, 9600, false); err == nil {
				if modem, err := reader.GetModemStatus(); err == nil {
					status.CTS = modem.CTS
					status.DSR = modem.DSR
					status.DCD = modem.DCD
					status.RI = modem.RI
					status.Connected = modem.DCD || modem.DSR
				}
				reader.Close()
			}
		}

		ports = append(ports, status)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ports": ports,
	})
}

// SystemInfo contains system health metrics
type SystemInfo struct {
	Hostname   string        `json:"hostname"`
	Uptime     int64         `json:"uptime_seconds"`
	CPU        CPUInfo       `json:"cpu"`
	Memory     MemoryInfo    `json:"memory"`
	Storage    []StorageInfo `json:"storage"`
	Network    []NetInfo     `json:"network"`
	GoRoutines int           `json:"goroutines"`
	Version    string        `json:"version"`
}

// CPUInfo contains CPU usage information
type CPUInfo struct {
	UsagePercent float64 `json:"usage_percent"`
	LoadAvg1     float64 `json:"load_avg_1"`
	LoadAvg5     float64 `json:"load_avg_5"`
	LoadAvg15    float64 `json:"load_avg_15"`
	NumCPU       int     `json:"num_cpu"`
}

// MemoryInfo contains memory usage information
type MemoryInfo struct {
	TotalMB     uint64  `json:"total_mb"`
	UsedMB      uint64  `json:"used_mb"`
	FreeMB      uint64  `json:"free_mb"`
	UsedPercent float64 `json:"used_percent"`
}

// StorageInfo contains disk usage information
type StorageInfo struct {
	Path        string  `json:"path"`
	TotalGB     float64 `json:"total_gb"`
	UsedGB      float64 `json:"used_gb"`
	FreeGB      float64 `json:"free_gb"`
	UsedPercent float64 `json:"used_percent"`
}

// NetInfo contains network interface information
type NetInfo struct {
	Name      string `json:"name"`
	MAC       string `json:"mac"`
	IP        string `json:"ip,omitempty"`
	LinkUp    bool   `json:"link_up"`
	Speed     string `json:"speed,omitempty"`
	RxBytes   uint64 `json:"rx_bytes"`
	TxBytes   uint64 `json:"tx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	TxPackets uint64 `json:"tx_packets"`
}

// handleSystem returns system health metrics
func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request) {
	info := SystemInfo{
		GoRoutines: runtime.NumGoroutine(),
		Version:    s.version,
	}

	// Hostname
	if h, err := os.Hostname(); err == nil {
		info.Hostname = h
	}

	// Uptime from /proc/uptime
	if data, err := os.ReadFile("/proc/uptime"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 1 {
			if uptime, err := strconv.ParseFloat(fields[0], 64); err == nil {
				info.Uptime = int64(uptime)
			}
		}
	}

	// CPU info
	info.CPU = getCPUInfo()

	// Memory info
	info.Memory = getMemoryInfo()

	// Storage info
	info.Storage = getStorageInfo()

	// Network info
	info.Network = getNetworkInfo()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(info)
}

// getCPUInfo reads CPU usage from /proc/stat and load averages
func getCPUInfo() CPUInfo {
	info := CPUInfo{
		NumCPU: runtime.NumCPU(),
	}

	// Load averages from /proc/loadavg
	if data, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(data))
		if len(fields) >= 3 {
			info.LoadAvg1, _ = strconv.ParseFloat(fields[0], 64)
			info.LoadAvg5, _ = strconv.ParseFloat(fields[1], 64)
			info.LoadAvg15, _ = strconv.ParseFloat(fields[2], 64)
		}
	}

	// Simple CPU usage estimate from load average
	info.UsagePercent = (info.LoadAvg1 / float64(info.NumCPU)) * 100
	if info.UsagePercent > 100 {
		info.UsagePercent = 100
	}

	return info
}

// getMemoryInfo reads memory info from /proc/meminfo
func getMemoryInfo() MemoryInfo {
	info := MemoryInfo{}

	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return info
	}

	memInfo := make(map[string]uint64)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			key := strings.TrimSuffix(fields[0], ":")
			val, _ := strconv.ParseUint(fields[1], 10, 64)
			memInfo[key] = val // Values are in kB
		}
	}

	info.TotalMB = memInfo["MemTotal"] / 1024
	info.FreeMB = (memInfo["MemFree"] + memInfo["Buffers"] + memInfo["Cached"]) / 1024
	info.UsedMB = info.TotalMB - info.FreeMB

	if info.TotalMB > 0 {
		info.UsedPercent = float64(info.UsedMB) / float64(info.TotalMB) * 100
	}

	return info
}

// getStorageInfo returns disk usage for key mount points
func getStorageInfo() []StorageInfo {
	var result []StorageInfo

	// Just check root - /var/log is typically on the same filesystem
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return result
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free

	info := StorageInfo{
		Path:    "/",
		TotalGB: float64(total) / (1024 * 1024 * 1024),
		UsedGB:  float64(used) / (1024 * 1024 * 1024),
		FreeGB:  float64(free) / (1024 * 1024 * 1024),
	}
	if total > 0 {
		info.UsedPercent = float64(used) / float64(total) * 100
	}

	result = append(result, info)
	return result
}

// getNetworkInfo returns info for physical ethernet interfaces
func getNetworkInfo() []NetInfo {
	var result []NetInfo

	interfaces, err := net.Interfaces()
	if err != nil {
		return result
	}

	// Read network stats from /proc/net/dev
	netStats := make(map[string][]uint64)
	if data, err := os.ReadFile("/proc/net/dev"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if !strings.Contains(line, ":") {
				continue
			}
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}
			name := strings.TrimSpace(parts[0])
			fields := strings.Fields(parts[1])
			if len(fields) >= 10 {
				rxBytes, _ := strconv.ParseUint(fields[0], 10, 64)
				rxPackets, _ := strconv.ParseUint(fields[1], 10, 64)
				txBytes, _ := strconv.ParseUint(fields[8], 10, 64)
				txPackets, _ := strconv.ParseUint(fields[9], 10, 64)
				netStats[name] = []uint64{rxBytes, rxPackets, txBytes, txPackets}
			}
		}
	}

	for _, iface := range interfaces {
		// Only include ethernet interfaces (enp*, eth*)
		if !strings.HasPrefix(iface.Name, "enp") && !strings.HasPrefix(iface.Name, "eth") {
			continue
		}

		info := NetInfo{
			Name:   iface.Name,
			MAC:    iface.HardwareAddr.String(),
			LinkUp: iface.Flags&net.FlagUp != 0,
		}

		// Get IP address
		if addrs, err := iface.Addrs(); err == nil {
			for _, addr := range addrs {
				if ipnet, ok := addr.(*net.IPNet); ok && ipnet.IP.To4() != nil {
					info.IP = ipnet.IP.String()
					break
				}
			}
		}

		// Get speed from sysfs
		speedPath := fmt.Sprintf("/sys/class/net/%s/speed", iface.Name)
		if data, err := os.ReadFile(speedPath); err == nil {
			speed := strings.TrimSpace(string(data))
			if speed != "" && speed != "-1" {
				info.Speed = speed + " Mbps"
			}
		}

		// Get stats
		if stats, ok := netStats[iface.Name]; ok && len(stats) >= 4 {
			info.RxBytes = stats[0]
			info.RxPackets = stats[1]
			info.TxBytes = stats[2]
			info.TxPackets = stats[3]
		}

		result = append(result, info)
	}

	return result
}

// handleSSE handles Server-Sent Events connections for real-time streaming
func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Check if client supports SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	channel := r.URL.Query().Get("channel")
	if channel == "" {
		channel = "all"
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	// Create client
	client := &SSEClient{
		channel: channel,
		send:    make(chan string, 64),
		done:    make(chan struct{}),
	}

	// Register client
	s.broker.register <- client

	// Ensure cleanup on disconnect
	defer func() {
		s.broker.unregister <- client
	}()

	// Send initial connection event
	fmt.Fprintf(w, "event: connected\ndata: {\"channel\":\"%s\"}\n\n", channel)
	flusher.Flush()

	// Send keepalive comment immediately
	fmt.Fprintf(w, ": keepalive\n\n")
	flusher.Flush()

	// Start keepalive ticker (every 15s)
	keepalive := time.NewTicker(15 * time.Second)
	defer keepalive.Stop()

	// Stream events
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			return

		case <-client.done:
			// Server shutting down
			return

		case line := <-client.send:
			// Send line as SSE event
			// Escape newlines in data for SSE format
			fmt.Fprintf(w, "event: line\ndata: %s\n\n", line)
			flusher.Flush()

		case <-keepalive.C:
			// Send keepalive comment to prevent connection timeout
			fmt.Fprintf(w, ": keepalive %d\n\n", time.Now().Unix())
			flusher.Flush()
		}
	}
}

// handleFeed returns the last N lines from a channel's log file (tail)
// Kept for backward compatibility and initial load
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

// handlePortsConfig returns all port configurations or adds a new port
func (s *Server) handlePortsConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ports := s.manager.GetPortConfigs()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ports": ports,
		})

	case http.MethodPost:
		// Add new port
		var portCfg config.PortConfig
		if err := json.NewDecoder(r.Body).Decode(&portCfg); err != nil {
			http.Error(w, "Invalid JSON body", http.StatusBadRequest)
			return
		}

		if err := s.manager.AddPort(portCfg); err != nil {
			if strings.Contains(err.Error(), "already") || strings.Contains(err.Error(), "required") {
				http.Error(w, err.Error(), http.StatusBadRequest)
			} else {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			return
		}

		s.logger.Info("Port added via API", "id", portCfg.ID())

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "ok",
			"message": fmt.Sprintf("Port %s added", portCfg.ID()),
		})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleAvailablePorts returns available serial ports not yet configured
func (s *Server) handleAvailablePorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	available := s.manager.GetAvailableSerialPorts()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"available_ports": available,
	})
}

// handlePortConfigAction handles port enable/disable/update actions
// Routes:
//   - PUT /api/ports/config/{id} - Update port settings
//   - POST /api/ports/config/{id}/enable - Enable port
//   - POST /api/ports/config/{id}/disable - Disable port
func (s *Server) handlePortConfigAction(w http.ResponseWriter, r *http.Request) {
	// Parse path: /api/ports/config/{id} or /api/ports/config/{id}/{action}
	path := strings.TrimPrefix(r.URL.Path, "/api/ports/config/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Port ID required", http.StatusBadRequest)
		return
	}

	// URL-decode the port ID (e.g., %2Fcdr -> /cdr)
	portID, err := decodePortID(parts[0])
	if err != nil {
		http.Error(w, "Invalid port ID", http.StatusBadRequest)
		return
	}

	// Determine action from path or method
	var action string
	if len(parts) >= 2 {
		action = parts[1]
	}

	switch {
	case action == "enable" && r.Method == http.MethodPost:
		s.handlePortEnable(w, r, portID)
	case action == "disable" && r.Method == http.MethodPost:
		s.handlePortDisable(w, r, portID)
	case action == "" && r.Method == http.MethodPut:
		s.handlePortUpdate(w, r, portID)
	case action == "" && r.Method == http.MethodGet:
		s.handlePortGet(w, r, portID)
	case action == "" && r.Method == http.MethodDelete:
		s.handlePortDelete(w, r, portID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// decodePortID decodes a URL-encoded port ID
func decodePortID(encoded string) (string, error) {
	// Handle URL encoding (e.g., %2F for /)
	decoded := encoded
	decoded = strings.ReplaceAll(decoded, "%2F", "/")
	decoded = strings.ReplaceAll(decoded, "%2f", "/")
	return decoded, nil
}

// handlePortGet returns a single port configuration
func (s *Server) handlePortGet(w http.ResponseWriter, r *http.Request, portID string) {
	ports := s.manager.GetPortConfigs()

	for _, port := range ports {
		if port.ID == portID {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(port)
			return
		}
	}

	http.Error(w, "Port not found", http.StatusNotFound)
}

// handlePortEnable enables a disabled port
func (s *Server) handlePortEnable(w http.ResponseWriter, r *http.Request, portID string) {
	if err := s.manager.EnablePort(portID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else if strings.Contains(err.Error(), "already enabled") {
			http.Error(w, err.Error(), http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.logger.Info("Port enabled via API", "port", portID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("Port %s enabled", portID),
	})
}

// handlePortDisable disables an enabled port
func (s *Server) handlePortDisable(w http.ResponseWriter, r *http.Request, portID string) {
	if err := s.manager.DisablePort(portID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else if strings.Contains(err.Error(), "already disabled") {
			http.Error(w, err.Error(), http.StatusConflict)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.logger.Info("Port disabled via API", "port", portID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("Port %s disabled", portID),
	})
}

// handlePortDelete removes a port configuration
func (s *Server) handlePortDelete(w http.ResponseWriter, r *http.Request, portID string) {
	if err := s.manager.DeletePort(portID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.logger.Info("Port deleted via API", "port", portID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("Port %s deleted", portID),
	})
}

// handlePortUpdate updates port configuration
func (s *Server) handlePortUpdate(w http.ResponseWriter, r *http.Request, portID string) {
	// Parse JSON body
	var updates map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if len(updates) == 0 {
		http.Error(w, "No updates provided", http.StatusBadRequest)
		return
	}

	// Validate updates
	if err := validatePortUpdates(updates); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := s.manager.UpdatePortConfig(portID, updates); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else if strings.Contains(err.Error(), "unknown config field") {
			http.Error(w, err.Error(), http.StatusBadRequest)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	s.logger.Info("Port config updated via API", "port", portID, "updates", updates)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "ok",
		"message": fmt.Sprintf("Port %s configuration updated", portID),
	})
}

// validatePortUpdates validates port configuration updates
func validatePortUpdates(updates map[string]interface{}) error {
	for key, value := range updates {
		switch key {
		case "baud_rate":
			if v, ok := value.(float64); ok {
				baud := int(v)
				validBauds := []int{0, 300, 1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200}
				valid := false
				for _, vb := range validBauds {
					if baud == vb {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("invalid baud_rate: %d", baud)
				}
			} else {
				return fmt.Errorf("baud_rate must be a number")
			}
		case "data_bits":
			if v, ok := value.(float64); ok {
				bits := int(v)
				if bits < 5 || bits > 8 {
					return fmt.Errorf("data_bits must be 5, 6, 7, or 8")
				}
			} else {
				return fmt.Errorf("data_bits must be a number")
			}
		case "parity":
			if v, ok := value.(string); ok {
				validParity := []string{"none", "odd", "even", "mark", "space"}
				valid := false
				for _, vp := range validParity {
					if v == vp {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("parity must be one of: none, odd, even, mark, space")
				}
			} else {
				return fmt.Errorf("parity must be a string")
			}
		case "stop_bits":
			if v, ok := value.(float64); ok {
				bits := int(v)
				if bits != 1 && bits != 2 {
					return fmt.Errorf("stop_bits must be 1 or 2")
				}
			} else {
				return fmt.Errorf("stop_bits must be a number")
			}
		case "use_flow_control":
			if value != nil {
				if _, ok := value.(bool); !ok {
					return fmt.Errorf("use_flow_control must be true, false, or null")
				}
			}
		case "listen_port":
			if v, ok := value.(float64); ok {
				port := int(v)
				if port < 0 || port > 65535 {
					return fmt.Errorf("listen_port must be 0-65535")
				}
			} else {
				return fmt.Errorf("listen_port must be a number")
			}
		case "description":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("description must be a string")
			}
		case "path":
			if v, ok := value.(string); ok {
				if !strings.HasPrefix(v, "/") {
					return fmt.Errorf("path must start with /")
				}
			} else {
				return fmt.Errorf("path must be a string")
			}
		case "side_designation":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("side_designation must be a string")
			}
		case "fips_code":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("fips_code must be a string")
			}
		case "vendor":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("vendor must be a string")
			}
		case "county":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("county must be a string")
			}
		default:
			return fmt.Errorf("unknown config field: %s", key)
		}
	}
	return nil
}

// handleEvents returns recent events from the NATS events stream
// Query params: count (default 50, max 200)
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Parse count parameter
	countStr := r.URL.Query().Get("count")
	count := 50
	if countStr != "" {
		if n, err := strconv.Atoi(countStr); err == nil && n > 0 {
			count = n
			if count > 200 {
				count = 200
			}
		}
	}

	// Get NATS connection from manager
	natsConn := s.manager.NATSConn()
	if natsConn == nil || !natsConn.IsConnected() {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"events": []interface{}{},
			"error":  "NATS not connected",
		})
		return
	}

	// Get JetStream context
	js, err := natsConn.Conn().JetStream()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"events": []interface{}{},
			"error":  "JetStream not available",
		})
		return
	}

	// Get events stream info to find last sequence
	streamInfo, err := js.StreamInfo("events")
	if err != nil {
		// Stream might not exist yet - return empty
		json.NewEncoder(w).Encode(map[string]interface{}{
			"events": []interface{}{},
			"error":  "Events stream not found",
		})
		return
	}

	// Calculate start sequence for last N messages
	lastSeq := streamInfo.State.LastSeq
	startSeq := uint64(1)
	if lastSeq > uint64(count) {
		startSeq = lastSeq - uint64(count) + 1
	}

	// Create ephemeral pull consumer starting at calculated sequence
	eventsSubject := s.manager.EventsSubject()
	sub, err := js.PullSubscribe(
		eventsSubject,
		"", // ephemeral (no durable name)
		nats.StartSequence(startSeq),
		nats.BindStream("events"),
	)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"events": []interface{}{},
			"error":  fmt.Sprintf("Failed to subscribe: %v", err),
		})
		return
	}
	defer sub.Unsubscribe()

	// Fetch messages with short timeout
	msgs, err := sub.Fetch(count, nats.MaxWait(2*time.Second))
	if err != nil && err != nats.ErrTimeout {
		s.logger.Warn("Error fetching events", "error", err)
	}

	// Parse and return events
	events := make([]json.RawMessage, 0, len(msgs))
	for _, msg := range msgs {
		events = append(events, json.RawMessage(msg.Data))
		msg.Ack()
	}

	json.NewEncoder(w).Encode(map[string]interface{}{
		"events": events,
		"count":  len(events),
		"stream": "events",
	})
}
