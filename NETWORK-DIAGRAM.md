add# NectarCollector Network Infrastructure

## Network Topology

```
        ┌──────────────────────────────────────────────────┐
        │      CDR Generator (Test Data Source)            │
        │      IP: 100.88.226.119                         │
        ├──────────────────────────────────────────────────┤
        │ Outputs:                                         │
        │  • /dev/ttyS4 (COM5): Vesta replay @ 2.5 CPM   │
        │  • /dev/ttyS5 (COM6): Viper synthetic @ 5 CPM  │
        │                                                  │
        │ Services:                                        │
        │  • CDR Generator Dashboard (Port 8080)          │
        │    http://100.88.226.119:8080/                 │
        │                                                  │
        │ Features:                                        │
        │  • Replay real CDR samples (loop)               │
        │  • Generate synthetic CDR data                  │
        │  • Configurable call rates (CPM)                │
        └──────────────────┬───────────────────────────────┘
                           │
                           │ Serial Cable (RS-232)
                           │ 9600 baud, 8N1
                           ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                       Serial Data Receiver (Collector Input)                 │
│                                                                              │
│  • Receives serial data at 9600 baud, 8N1 from CDR Generator               │
│  • Connected via Y-cable for redundancy                                     │
└──────────────────────────────────┬──────────────────────────────────────────┘
                                   │
                                   │ Y-Cable Split
                    ┌──────────────┴──────────────┐
                    │                             │
                    ▼                             ▼
┌───────────────────────────────┐   ┌───────────────────────────────┐
│   COLLECTOR A (Primary)       │   │   COLLECTOR B (Secondary)     │
│   IP: 100.104.43.22          │   │   IP: 100.73.92.66           │
├───────────────────────────────┤   ├───────────────────────────────┤
│ Hardware:                     │   │ Hardware:                     │
│  • Device: /dev/ttyS4 (COM5) │   │  • Device: /dev/ttyS4 (COM5) │
│  • FIPS: 1429010003-A1       │   │  • FIPS: 1429010002-A5       │
│  • Baud: 9600 (auto-detect)  │   │  • Baud: 9600 (auto-detect)  │
│                               │   │                               │
│ Services:                     │   │ Services:                     │
│  • NectarCollector            │   │  • NectarCollector            │
│  • HoneyView (Port 8080)     │   │  • HoneyView (Port 8080)     │
│                               │   │                               │
│ Outputs:                      │   │ Outputs:                      │
│  • Log: /var/log/nectarcol... │   │  • Log: /var/log/nectarcol... │
│  • NATS: nats://100.110...   │   │  • NATS: nats://100.110...   │
└───────────────┬───────────────┘   └───────────────┬───────────────┘
                │                                   │
                │          Tailscale VPN            │
                │          (Encrypted)              │
                │                                   │
                └──────────────┬────────────────────┘
                               │
                               ▼
        ┌──────────────────────────────────────────────────┐
        │      NATS Central Hub & Monitoring Server        │
        │      IP: 100.110.134.113                        │
        ├──────────────────────────────────────────────────┤
        │ NATS Server:                                     │
        │  • Port 4222: NATS Protocol (messaging)         │
        │  • Port 8222: HTTP Monitoring API               │
        │  • JetStream: Enabled (1GB mem, 10GB disk)     │
        │  • Server Name: nats-central-hub                │
        │                                                  │
        │ Metrics Collection:                              │
        │  • NATS Surveyor (Port 7777)                    │
        │    - Exports NATS metrics to Prometheus         │
        │  • Connection Exporter (Port 9100)              │
        │    - Per-connection metrics                      │
        │  • Prometheus (Port 9090)                       │
        │    - Time-series metrics database               │
        │                                                  │
        │ Visualization:                                   │
        │  • Grafana (Port 3000)                          │
        │    - Web dashboards                             │
        │    - Username: admin / Password: admin          │
        │                                                  │
        │ Storage:                                         │
        │  • JetStream: /var/lib/nats/jetstream/         │
        │  • Prometheus: /var/lib/prometheus/            │
        │  • Grafana: /var/lib/grafana/                  │
        └──────────────────────────────────────────────────┘
```

## Detailed Component Breakdown

### CDR Generator (Test Data Source) - 100.88.226.119
**Hardware:**
- Serial Outputs:
  - `/dev/ttyS4` (COM5): Connected to Collector B
  - `/dev/ttyS5` (COM6): Viper synthetic generator

**Software Stack:**
- **CDR Generator Service**: `/usr/local/bin/cdrgenerator`
  - Config: `/etc/cdrgenerator/config.json`
  - Auto-start: Enabled
  - Instance ID: `ne-datacenter-01`

- **Dashboard**: Port 8080
  - Web UI: `http://100.88.226.119:8080/`
  - Health API: `http://100.88.226.119:8080/health`
  - System Ports API: `http://100.88.226.119:8080/api/sysports`
  - Records API: `http://100.88.226.119:8080/api/records?device=/dev/ttyS4`

**Output Channels:**
1. **Channel 1 (COM5)**: Vesta Format
   - Mode: `replay` (loops sample file)
   - Sample: `samples/Vesta/vestasample.csv`
   - Rate: 2.5 calls per minute
   - Target: Collector B `/dev/ttyS4`

2. **Channel 2 (COM6)**: Viper Format
   - Mode: `synthetic` (generates data)
   - System ID: `NE-PSAP-001`
   - Rate: 5 calls per minute
   - Agents: 15 simulated agents

**Features:**
- Replay real CDR samples in loop mode
- Generate synthetic CDR data with realistic patterns
- Configurable call rates (calls per minute)
- Timing jitter (20%) to simulate real-world variance
- Web dashboard with real-time stats
- Recent records viewer (clickable rows)
- System COM port monitoring

### Serial Data Receiver (Y-Cable Input)
- **Function**: Receives serial data from CDR Generator
- **Configuration**: 9600 baud, 8N1, auto-detected flow control
- **Redundancy**: Y-cable splits signal to both collectors simultaneously
- **Source**: CDR Generator COM5 → Y-cable → Both collectors

### Collector A (Primary) - 100.104.43.22
**Hardware:**
- Serial Device: `/dev/ttyS4` (COM5)
- FIPS Code: `1429010003-A1`
- Designation: `A1`

**Software Stack:**
- **NectarCollector Service**: `/usr/local/bin/nectarcollector`
  - Config: `/root/nectarcollector-config.json`
  - Systemd: `nectarcollector.service`
  - Auto-start: Enabled

- **HoneyView Dashboard**: Port 8080
  - Health API: `http://100.104.43.22:8080/api/health`
  - Stats API: `http://100.104.43.22:8080/api/stats`
  - Feed API: `http://100.104.43.22:8080/api/feed?channel=1429010003-A1`
  - Web UI: `http://100.104.43.22:8080/`

**Data Flow:**
1. Reads serial data from `/dev/ttyS4` line-by-line
2. Prepends header: `[1429010003][A1][2025-12-04 18:00:00.000]`
3. Writes to rotating log: `/var/log/nectarcollector/1429010003-A1.log`
4. Publishes to NATS: `serial.1429010003-A1`

### Collector B (Secondary) - 100.73.92.66
**Hardware:**
- Serial Device: `/dev/ttyS4` (COM5)
- FIPS Code: `1429010002-A5`
- Designation: `A5`

**Software Stack:**
- **NectarCollector Service**: `/usr/local/bin/nectarcollector`
  - Config: `/root/nectarcollector-config.json`
  - Systemd: `nectarcollector.service`
  - Auto-start: Enabled

- **HoneyView Dashboard**: Port 8080
  - Health API: `http://100.73.92.66:8080/api/health`
  - Stats API: `http://100.73.92.66:8080/api/stats`
  - Feed API: `http://100.73.92.66:8080/api/feed?channel=1429010002-A5`
  - Web UI: `http://100.73.92.66:8080/`

**Data Flow:**
1. Reads serial data from `/dev/ttyS4` line-by-line
2. Prepends header: `[1429010002][A5][2025-12-04 18:00:00.000]`
3. Writes to rotating log: `/var/log/nectarcollector/1429010002-A5.log`
4. Publishes to NATS: `serial.1429010002-A5`

### NATS Central Hub - 100.110.134.113

#### NATS Server (Core Messaging)
- **Binary**: `/usr/local/bin/nats-server` v2.10.7
- **Config**: `/etc/nats/nats-server.conf`
- **Service**: `nats-server.service`
- **Ports**:
  - `4222`: NATS client protocol (collectors connect here)
  - `8222`: HTTP monitoring/management API
- **JetStream**:
  - Enabled: Yes
  - Store: `/var/lib/nats/jetstream/`
  - Memory Limit: 1GB
  - Disk Limit: 10GB
- **Subjects**:
  - `serial.1429010003-A1` (Collector A)
  - `serial.1429010002-A5` (Collector B)

#### NATS Surveyor (Metrics Exporter)
- **Binary**: `/usr/local/bin/nats-surveyor` v0.9.5
- **Service**: `nats-surveyor.service`
- **Port**: `7777`
- **Function**: Exports NATS server metrics in Prometheus format
- **Metrics Prefix**: `nats_`
- **Scrape Interval**: 15 seconds
- **Key Metrics**:
  - `nats_core_account_conn_count`: Active connections
  - `nats_core_account_msgs_recv`: Total messages received
  - `nats_core_account_bytes_recv`: Total bytes received

#### NATS Connection Exporter (Custom)
- **Binary**: `/usr/local/bin/nats-connection-exporter`
- **Service**: `nats-connection-exporter.service`
- **Port**: `9100`
- **Source**: `/Users/alexwarner/Documents/Git/nectarcollector/tools/nats-connection-exporter.go`
- **Function**: Exports per-connection metrics from NATS `/connz` endpoint
- **Key Metrics**:
  - `nats_connection_in_msgs{ip, name, cid, port}`: Messages per connection
  - `nats_connection_in_bytes{ip, name, cid, port}`: Bytes per connection
  - `nats_connection_out_msgs{ip, name, cid, port}`: Outbound messages
  - `nats_connection_out_bytes{ip, name, cid, port}`: Outbound bytes
  - `nats_connection_pending_bytes{ip, name, cid, port}`: Pending bytes
  - `nats_connection_subscriptions{ip, name, cid, port}`: Active subscriptions

#### Prometheus (Time-Series Database)
- **Binary**: `/usr/local/bin/prometheus` v3.0.1
- **Config**: `/etc/prometheus/prometheus.yml`
- **Service**: `prometheus.service`
- **Port**: `9090`
- **Storage**: `/var/lib/prometheus/`
- **Scrape Jobs**:
  - `prometheus`: Self-monitoring (localhost:9090)
  - `nats-surveyor`: NATS metrics (localhost:7777)
  - `nats-connections`: Per-connection metrics (localhost:9100)
- **Scrape Interval**: 15 seconds
- **Retention**: Default (15 days)

#### Grafana (Visualization)
- **Version**: v12.3.0
- **Service**: `grafana-server.service`
- **Port**: `3000`
- **Web UI**: `http://100.110.134.113:3000/`
- **Credentials**: `admin` / `admin`
- **Data Sources**:
  - Prometheus: `http://localhost:9090`
- **Dashboards**:
  1. **NATS Server Monitoring**
     - URL: `/d/nats/nats-server-monitoring`
     - Metrics: Overall server health, total connections, message rates
  2. **NATS Connection Details**
     - URL: `/d/nats-connections/nats-server-connection-details`
     - Metrics: Per-connection message/byte rates, connection info table

## Data Flow Diagram

```
Serial Port (/dev/ttyS4)
         │
         ├─ Autobaud Detection (300-115200 baud)
         ├─ Flow Control Detection (RTS/CTS vs none)
         │
         ▼
  NectarCollector
         │
         ├─ Line-by-line buffering
         ├─ Header construction: [FIPS][A-Designation][Timestamp]
         │
         ├──────┬──────────────────────────────┐
         │      │                              │
         ▼      ▼                              ▼
    Log File   NATS Publish              Stats Update
    (Rotating)  (JetStream)              (In-Memory)
         │      │                              │
         │      ├─ Subject: serial.<FIPS>-<A> │
         │      ├─ Reliable delivery           │
         │      │                              │
         │      ▼                              ▼
         │  NATS Server                  HoneyView API
         │  (100.110.134.113:4222)       (Port 8080)
         │      │
         │      ├─ JetStream Storage
         │      ├─ Message Persistence
         │      │
         │      ▼
         │  Monitoring Endpoints
         │      │
         │      ├─ /varz (server info)
         │      ├─ /connz (connections)
         │      ├─ /jsz (jetstream)
         │      │
         │      ▼
         ├─ NATS Surveyor (port 7777)
         ├─ Connection Exporter (port 9100)
         │      │
         │      ▼
         │  Prometheus (port 9090)
         │      │
         │      ▼
         │  Grafana Dashboards (port 3000)
         │
         ▼
    Log Analysis Tools
    (future: ElasticSearch, Splunk, etc.)
```

## Network Communication Matrix

| From                | To                  | Protocol | Port | Purpose                        |
|---------------------|---------------------|----------|------|--------------------------------|
| CDR Generator       | Collector B         | RS-232   | N/A  | Send test serial data          |
| Collector A         | NATS Server         | NATS     | 4222 | Publish serial data            |
| Collector B         | NATS Server         | NATS     | 4222 | Publish serial data            |
| NATS Surveyor       | NATS Server         | HTTP     | 8222 | Scrape server metrics          |
| Connection Exporter | NATS Server         | HTTP     | 8222 | Scrape connection metrics      |
| Prometheus          | NATS Surveyor       | HTTP     | 7777 | Scrape NATS metrics            |
| Prometheus          | Connection Exporter | HTTP     | 9100 | Scrape connection metrics      |
| Grafana             | Prometheus          | HTTP     | 9090 | Query metrics                  |
| User Browser        | CDR Generator       | HTTP     | 8080 | View CDR dashboard             |
| User Browser        | Grafana             | HTTP     | 3000 | View dashboards                |
| User Browser        | Collector A         | HTTP     | 8080 | View HoneyView dashboard       |
| User Browser        | Collector B         | HTTP     | 8080 | View HoneyView dashboard       |

## Port Reference

### CDR Generator (100.88.226.119)
- **8080**: CDR Generator web dashboard (HTTP)

### Collector A (100.104.43.22)
- **8080**: HoneyView web dashboard (HTTP)

### Collector B (100.73.92.66)
- **8080**: HoneyView web dashboard (HTTP)

### NATS Server (100.110.134.113)
- **4222**: NATS client connections (NATS protocol)
- **8222**: NATS monitoring API (HTTP)
- **7777**: NATS Surveyor metrics (Prometheus HTTP)
- **9090**: Prometheus web UI and API (HTTP)
- **9100**: Connection Exporter metrics (Prometheus HTTP)
- **3000**: Grafana web UI (HTTP)

## System Requirements

### CDR Generator
- **OS**: Linux (Debian-based)
- **Go**: 1.23.4+
- **Disk**: ~15MB for binary, 1GB for samples/logs
- **Memory**: ~20MB
- **CPU**: Minimal (<1% typical)
- **Serial**: RS-232 compatible ports (2+ for multi-channel output)

### Collectors (A & B)
- **OS**: Linux (Debian-based)
- **Go**: 1.23.4+
- **Disk**: ~100MB for binary, 1-10GB for logs (with rotation)
- **Memory**: ~50MB per collector
- **CPU**: Minimal (<1% typical)
- **Serial**: RS-232 compatible port

### NATS Central Hub
- **OS**: Linux (Debian-based)
- **Disk**:
  - 20GB+ for JetStream storage
  - 10GB+ for Prometheus metrics
  - 5GB+ for Grafana
- **Memory**: 2GB+ recommended (JetStream uses 1GB max)
- **CPU**: 2+ cores recommended
- **Network**: 100Mbps+ (low bandwidth, <1Mbps typical)

## Monitoring URLs

### Live Dashboards
- **CDR Generator**: http://100.88.226.119:8080/
- **Grafana Home**: http://100.110.134.113:3000/
- **NATS Overview**: http://100.110.134.113:3000/d/nats/nats-server-monitoring
- **Connection Details**: http://100.110.134.113:3000/d/nats-connections/nats-server-connection-details
- **Prometheus UI**: http://100.110.134.113:9090/
- **Collector A HoneyView**: http://100.104.43.22:8080/
- **Collector B HoneyView**: http://100.73.92.66:8080/

### API Endpoints
- **CDR Generator Health**: http://100.88.226.119:8080/health
- **CDR Generator Sys Ports**: http://100.88.226.119:8080/api/sysports
- **CDR Generator Records**: http://100.88.226.119:8080/api/records?device=/dev/ttyS4
- **NATS Server Info**: http://100.110.134.113:8222/varz
- **NATS Connections**: http://100.110.134.113:8222/connz
- **NATS JetStream**: http://100.110.134.113:8222/jsz
- **NATS Metrics**: http://100.110.134.113:7777/metrics
- **Connection Metrics**: http://100.110.134.113:9100/metrics
- **Prometheus Metrics**: http://100.110.134.113:9090/metrics
- **Collector A Health**: http://100.104.43.22:8080/api/health
- **Collector A Stats**: http://100.104.43.22:8080/api/stats
- **Collector B Health**: http://100.73.92.66:8080/api/health
- **Collector B Stats**: http://100.73.92.66:8080/api/stats

## Redundancy & Failover

### Y-Cable Redundancy
- **Topology**: Passive Y-cable splits serial signal to both collectors
- **Independence**: Each collector operates independently
- **Data Integrity**: Both collectors receive identical data stream
- **Validated**: Y-cable disconnect tests passed 100% (see TEST-RESULTS.md)
- **Recovery Time**: <3 seconds on cable reconnection

### NATS High Availability
- **Current**: Single NATS server
- **Future**: Can add NATS cluster for HA (3+ servers)
- **JetStream**: Persistent storage survives server restart
- **Reconnection**: Collectors auto-reconnect with exponential backoff

### Data Persistence
1. **Local Logs**: Rotating logs on each collector (primary backup)
2. **NATS JetStream**: Persistent message storage (10GB disk)
3. **Prometheus**: 15-day metric retention (configurable)

## Operational Notes

### Service Management

**CDR Generator (100.88.226.119):**
```bash
# Check status
systemctl status cdrgenerator

# View logs
journalctl -u cdrgenerator -f

# Restart service
systemctl restart cdrgenerator

# View application logs
tail -f cdrgenerator.log

# View dashboard
curl http://localhost:8080/health | jq
```

**Collectors (run on each collector box):**
```bash
# Check status
systemctl status nectarcollector

# View logs
journalctl -u nectarcollector -f

# Restart service
systemctl restart nectarcollector

# View application logs
tail -f /var/log/nectarcollector/[FIPS]-[A].log
```

**NATS Server:**
```bash
# Check all services
systemctl status nats-server
systemctl status nats-surveyor
systemctl status nats-connection-exporter
systemctl status prometheus
systemctl status grafana-server

# View NATS logs
journalctl -u nats-server -f

# Check NATS connections
curl http://localhost:8222/connz | jq
```

### Backup Recommendations
1. **Collector Logs**: `/var/log/nectarcollector/*.log` (daily/weekly backup)
2. **NATS Config**: `/etc/nats/nats-server.conf`
3. **JetStream Data**: `/var/lib/nats/jetstream/` (periodic backup)
4. **Grafana Dashboards**: Export via UI or `/var/lib/grafana/`
5. **Prometheus Data**: `/var/lib/prometheus/` (optional, metrics are regenerated)

### Performance Baselines
- **Serial Data Rate**: ~10 lines/sec per collector (~300 bytes/sec)
- **NATS Message Rate**: ~20 msgs/sec (both collectors combined)
- **NATS Bandwidth**: <1 KB/sec typical
- **Collector Memory**: ~50MB RSS per instance
- **NATS Server Memory**: ~100MB + JetStream storage
- **Prometheus Memory**: ~500MB typical

## Future Enhancements

### Potential Additions
1. **NATS Clustering**: Add 2 more NATS servers for HA
2. **Stream Processing**: Add NATS consumer for real-time analysis
3. **Alerting**: Configure Grafana alerts for collector failures
4. **Log Aggregation**: Forward logs to ElasticSearch/Splunk
5. **Geo-Redundancy**: Replicate JetStream to remote datacenter
6. **Additional Collectors**: Scale to 16 A-designations (A1-A16)
7. **Authentication**: Add NATS authentication (tokens/JWT)
8. **TLS**: Enable TLS encryption for NATS connections

---

**Document Version**: 1.0
**Last Updated**: 2025-12-04
**Author**: Alex Warner
**System Status**: Production-ready, validated via Y-cable disconnect testing
