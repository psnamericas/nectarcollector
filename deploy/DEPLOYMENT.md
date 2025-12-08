# NectarCollector Deployment Guide

```
         _
        /_/_      .'''.
     =O(_)))) ~=='  _  '=~      "Gathering data, one byte at a time"
        \_\       /  \
                  \__/
```

Complete end-to-end deployment guide for NectarCollector on Maple Systems IPC2210A/IPC2115A industrial PCs.

---

## Table of Contents

1. [Quick Reference](#quick-reference)
2. [Prerequisites](#prerequisites)
3. [Step 1: Create Site Config](#step-1-create-site-config)
4. [Step 2: Build the Binary](#step-2-build-the-binary)
5. [Step 3: Push Files to Target](#step-3-push-files-to-target)
6. [Step 4: Run Setup Script](#step-4-run-setup-script)
7. [Step 5: Verify Deployment](#step-5-verify-deployment)
8. [Operations](#operations)
9. [Troubleshooting](#troubleshooting)
10. [File Reference](#file-reference)

---

## Quick Reference

### Deploy New Site (TL;DR)

```bash
# 1. Create config
cp deploy/configs/sites/TEMPLATE.json deploy/configs/sites/setup-psna-ne-mysite-01.json
# Edit with: hostname, fips_code, vendor, county, ports

# 2. Build binary
GOOS=linux GOARCH=amd64 go build -o nectarcollector-linux

# 3. Push to target (use IP for first deploy)
TARGET="root@192.168.1.100"
ssh $TARGET "mkdir -p /opt/nectarcollector"
scp nectarcollector-linux $TARGET:/opt/nectarcollector/nectarcollector
scp deploy/scripts/setup.sh $TARGET:/opt/nectarcollector/
scp deploy/configs/sites/setup-psna-ne-mysite-01.json $TARGET:/opt/nectarcollector/setup-config.json

# 4. Run setup (interactive - answer prompts)
ssh $TARGET "chmod +x /opt/nectarcollector/*.sh /opt/nectarcollector/nectarcollector"
ssh -t $TARGET "/opt/nectarcollector/setup.sh"

# 5. Verify (after Tailscale setup, use hostname)
ssh root@psna-ne-mysite-01 "systemctl status nectarcollector"
curl -u psna:yourpassword http://psna-ne-mysite-01:8080/api/stats
```

### Update Existing Deployment

```bash
# Binary only
GOOS=linux GOARCH=amd64 go build -o nectarcollector-linux
scp nectarcollector-linux root@psna-ne-mysite-01:/usr/local/bin/nectarcollector
ssh root@psna-ne-mysite-01 "systemctl restart nectarcollector"

# Config change - edit local file, then:
scp deploy/configs/sites/setup-psna-ne-mysite-01.json root@psna-ne-mysite-01:/opt/nectarcollector/setup-config.json
ssh root@psna-ne-mysite-01 "/opt/nectarcollector/setup.sh"
```

---

## Prerequisites

### Hardware
- Maple Systems IPC2210A or IPC2115A with Ubuntu 24.04
- Serial ports connected to CDR sources (COM2-COM4 / ttyS1-ttyS3)
- Network connectivity (Ethernet)

### Development Machine (Mac/Linux)
- Go 1.21+ installed
- SSH access to target network

### Information Needed

| Field | Description | Example |
|-------|-------------|---------|
| **Hostname** | `psna-{state}-{region}-{site}-##` | `psna-ne-southcentralpan-kearney-01` |
| **FIPS Code** | 10-digit PSAP identifier | `1314010001` |
| **Vendor** | CDR system type | `vesta`, `intrado`, `motorola`, `zetron`, `viper` |
| **County** | County name (lowercase) | `kearney` |
| **Ports** | Serial ports with CDR feeds | `ttyS1`, `ttyS2` |
| **Dashboard Password** | For HoneyView web UI | `nectar2025` |

### Serial Port Mapping

| Linux Device | COM Port | Usage |
|--------------|----------|-------|
| `/dev/ttyS0` | COM1 | **Reserved for console** (emergency access) |
| `/dev/ttyS1` | COM2 | CDR capture |
| `/dev/ttyS2` | COM3 | CDR capture |
| `/dev/ttyS3` | COM4 | CDR capture |

---

## Step 1: Create Site Config

Create a simple JSON config for the site:

```bash
# Copy template
cp deploy/configs/sites/TEMPLATE.json deploy/configs/sites/setup-psna-ne-mysite-01.json

# Edit the file
```

### Config Format

```json
{
  "hostname": "psna-ne-kearney-01",
  "fips_code": "1314010001",
  "vendor": "viper",
  "county": "kearney",
  "ports": ["ttyS1"],
  "dashboard_user": "psna",
  "dashboard_pass": "nectar2025"
}
```

### Field Reference

| Field | Required | Description |
|-------|----------|-------------|
| `hostname` | Yes | System hostname, format: `psna-{state}-{region/county}-##` |
| `fips_code` | Yes | 10-digit PSAP FIPS identifier |
| `vendor` | Yes | CDR vendor: `vesta`, `intrado`, `motorola`, `zetron`, `viper` |
| `county` | Yes | County name (lowercase, no spaces) |
| `ports` | Yes | Array of serial ports: `["ttyS1"]` or `["ttyS1", "ttyS2"]` |
| `dashboard_user` | Yes | HoneyView username (default: `psna`) |
| `dashboard_pass` | Yes | HoneyView password |

### What Gets Generated

The setup script converts this simple config into a full NectarCollector config:

- Each port gets an A-designation (A1, A2, A3...)
- Baud rate set to 0 (auto-detect)
- NATS subject derived from state: `ne.cdr` for Nebraska
- All sensible defaults for logging, recovery, monitoring

---

## Step 2: Build the Binary

From the nectarcollector repo root:

```bash
# Build for Linux AMD64 (Maple Systems IPC)
GOOS=linux GOARCH=amd64 go build -o nectarcollector-linux

# Verify
file nectarcollector-linux
# Expected: ELF 64-bit LSB executable, x86-64
```

---

## Step 3: Push Files to Target

### First Deployment (via IP)

For a new machine, connect via local network IP:

```bash
# Find the machine's IP (from same network)
nmap -sn 192.168.1.0/24 | grep -B2 "Maple\|Ubuntu"
# Or check DHCP leases on router

# Set target
TARGET="root@192.168.1.100"

# Create directory and push files
ssh $TARGET "mkdir -p /opt/nectarcollector"

scp nectarcollector-linux $TARGET:/opt/nectarcollector/nectarcollector
scp deploy/scripts/setup.sh $TARGET:/opt/nectarcollector/
scp deploy/configs/sites/setup-psna-ne-mysite-01.json $TARGET:/opt/nectarcollector/setup-config.json

# Make executable
ssh $TARGET "chmod +x /opt/nectarcollector/setup.sh /opt/nectarcollector/nectarcollector"
```

### Subsequent Deployments (via Tailscale hostname)

After Tailscale is configured:

```bash
TARGET="root@psna-ne-mysite-01"
# Same commands as above
```

---

## Step 4: Run Setup Script

### Interactive Setup

SSH into the target and run setup:

```bash
ssh -t $TARGET "/opt/nectarcollector/setup.sh"
```

The `-t` flag allocates a TTY for interactive prompts.

### What the Script Does

| Phase | Description |
|-------|-------------|
| 0 | Pre-flight checks (root, Ubuntu, internet) |
| 1 | Set system hostname |
| 2 | Create users (`psna`, `nectarcollector`, `nats`) |
| 3 | Install & configure Tailscale |
| 4 | SSH lockdown (Tailscale-only access) |
| 5 | System packages, timezone (UTC), serial console |
| 6 | Install Go 1.23.4 |
| 7 | Install NATS Server 2.10.22 with JetStream |
| 8 | Install NectarCollector binary + generate config |
| 9 | Document network interfaces (MAC addresses) |
| 10 | Document serial ports |
| 11 | Summary + optional reboot |

### Interactive Prompts

The script will prompt for:

1. **"Ready to begin setup?"** - Confirm to start
2. **psna user password** - Set password for `psna` login user (first run only)
3. **Tailscale auth** - Opens browser or provides auth URL (first run only)
4. **Reboot?** - Recommended: Yes (applies GRUB/serial console changes)

### Non-Interactive (CI/CD)

For automated deployments, pipe responses:

```bash
# Warning: This skips some confirmations
echo -e "y\n" | ssh $TARGET "/opt/nectarcollector/setup.sh"
```

---

## Step 5: Verify Deployment

### After Reboot

Connect via Tailscale:

```bash
ssh root@psna-ne-mysite-01
```

### Check Services

```bash
# Both should show "active (running)"
systemctl status nats-server
systemctl status nectarcollector
```

Expected output:
```
● nectarcollector.service - NectarCollector CDR Capture Service
     Loaded: loaded (/etc/systemd/system/nectarcollector.service; enabled)
     Active: active (running) since ...
```

### Check Dashboard API

```bash
# From anywhere with Tailscale access
curl -u psna:nectar2025 http://psna-ne-mysite-01:8080/api/stats | jq
```

Expected output:
```json
{
  "channels": [
    {
      "device": "/dev/ttyS1",
      "a_designation": "A1",
      "fips_code": "1314010001",
      "state": "detecting",
      "stats": { ... }
    }
  ],
  "nats_connected": true
}
```

States:
- `detecting` - Waiting for data / auto-detecting baud rate
- `running` - Actively capturing data
- `reconnecting` - Lost connection, retrying

### Check HoneyView Dashboard

Open in browser:
```
http://psna-ne-mysite-01:8080
```

Login with configured credentials.

### Check Logs

```bash
# Application logs
journalctl -u nectarcollector -f

# CDR data files
ls -la /var/log/nectarcollector/
tail -f /var/log/nectarcollector/1314010001-A1.log
```

### Check NATS & JetStream

```bash
# NATS CLI is installed by setup
nats account info

# List JetStream streams
nats stream ls

# Check CDR stream stats
nats stream info cdr

# Check health stream stats
nats stream info health

# Get last message from CDR stream
nats stream get cdr 1

# Subscribe to live CDR data (Core NATS - ephemeral)
nats sub "ne.cdr.>"
```

**JetStream Streams Created by Setup:**

| Stream | Subjects | Max Size | TTL | Purpose |
|--------|----------|----------|-----|---------|
| `cdr` | `*.cdr.>` | 50 GB | None | Durable CDR storage |
| `health` | `*.health.>` | 5 GB | 30 days | Health heartbeats |

---

## Operations

### Service Management

```bash
# Status
systemctl status nectarcollector
systemctl status nats-server

# Restart
systemctl restart nectarcollector

# Stop/Start
systemctl stop nectarcollector
systemctl start nectarcollector

# View logs
journalctl -u nectarcollector -f
journalctl -u nats-server -f
```

### Update Binary

```bash
# Build new version
GOOS=linux GOARCH=amd64 go build -o nectarcollector-linux

# Deploy
scp nectarcollector-linux root@psna-ne-mysite-01:/usr/local/bin/nectarcollector
ssh root@psna-ne-mysite-01 "chmod +x /usr/local/bin/nectarcollector && systemctl restart nectarcollector"
```

### Update Configuration

```bash
# Edit local config
vim deploy/configs/sites/setup-psna-ne-mysite-01.json

# Push and regenerate
scp deploy/configs/sites/setup-psna-ne-mysite-01.json root@psna-ne-mysite-01:/opt/nectarcollector/setup-config.json
ssh -t root@psna-ne-mysite-01 "/opt/nectarcollector/setup.sh"
```

### Manual Config Edit (emergency)

```bash
ssh root@psna-ne-mysite-01
vim /etc/nectarcollector/config.json
systemctl restart nectarcollector
```

### Access Methods (in order of preference)

| Method | Command | When |
|--------|---------|------|
| Tailscale SSH | `ssh root@{hostname}` | Primary access |
| Tailscale IP | `ssh root@100.x.x.x` | If DNS issues |
| Local Network | `ssh root@{local-ip}` | Same LAN only |
| Serial Console | `screen /dev/cu.usbserial-* 115200` | Emergency/no network |
| Physical | HDMI + USB keyboard | Last resort |

---

## Troubleshooting

### NectarCollector Won't Start

```bash
# Check logs
journalctl -u nectarcollector -n 50 --no-pager

# Common issues:
# 1. NATS not running
systemctl status nats-server
systemctl start nats-server

# 2. Serial port permissions
ls -la /dev/ttyS*
groups nectarcollector  # Should include 'dialout'

# 3. Config error - test manually
/usr/local/bin/nectarcollector -config /etc/nectarcollector/config.json -debug
```

### No Data Flowing

```bash
# Check channel state via API
curl -s -u psna:nectar2025 http://localhost:8080/api/stats | jq '.channels[].state'

# "detecting" = no data or wrong baud rate
# "running" = capturing data
# "reconnecting" = port error

# Test serial port manually
cat /dev/ttyS1  # Should see CDR data (Ctrl+C to stop)

# Check permissions
ls -la /dev/ttyS1
# Should be: crw-rw---- 1 root dialout ...
```

### NATS Issues

```bash
# Check NATS is running
systemctl status nats-server

# Check JetStream
nats account info

# Check NATS logs
journalctl -u nats-server -n 50

# Permission denied on log file?
ls -la /var/log/nectarcollector/
# Should be: drwxrwxrwx root root
chmod 777 /var/log/nectarcollector  # If needed
```

### Baud Rate Detection Fails

If auto-detection doesn't work (data visible with `cat` but state stays "detecting"):

1. **Check data is printable ASCII** - binary protocols won't auto-detect
2. **Set explicit baud rate** in config:

```bash
# Edit config
vim /etc/nectarcollector/config.json

# Change baud_rate from 0 to known value:
# "baud_rate": 9600

systemctl restart nectarcollector
```

Common baud rates: `9600`, `19200`, `38400`, `57600`, `115200`

### Tailscale Issues

```bash
# Check status
tailscale status

# Re-authenticate
tailscale up

# Check IP
tailscale ip -4
```

### Can't SSH In

Try in order:
1. `ssh root@{hostname}` (Tailscale DNS)
2. `ssh root@{tailscale-ip}` (Tailscale IP from `tailscale ip -4`)
3. `ssh root@{local-ip}` (from same LAN)
4. Serial console (connect USB-serial adapter to COM1)
5. Physical access (HDMI + keyboard)

---

## File Reference

### Local Development (Mac)

```
nectarcollector/
├── deploy/
│   ├── DEPLOYMENT.md              # This guide
│   ├── configs/
│   │   ├── sites/
│   │   │   ├── TEMPLATE.json      # Copy for new sites
│   │   │   └── setup-*.json       # Site-specific configs
│   │   ├── streams/               # NATS stream definitions (reference)
│   │   └── nats-server.conf       # NATS config (reference)
│   ├── scripts/
│   │   └── setup.sh               # Main deployment script
│   ├── services/
│   │   ├── nectarcollector.service  # Systemd unit (reference)
│   │   └── nats-server.service      # Systemd unit (reference)
│   └── docs/
│       └── FAILURE_MODES.md       # Error handling documentation
└── nectarcollector-linux          # Built binary (gitignored)
```

### Target Server (Linux)

| Path | Purpose |
|------|---------|
| `/opt/nectarcollector/` | Setup staging (script, config, original binary) |
| `/usr/local/bin/nectarcollector` | Production binary |
| `/usr/local/bin/nats-server` | NATS server binary |
| `/usr/local/bin/nats` | NATS CLI tool |
| `/etc/nectarcollector/config.json` | NectarCollector runtime config |
| `/etc/nectarcollector/nats-server.conf` | NATS server config |
| `/etc/nectarcollector/network-info.txt` | MAC addresses (documentation) |
| `/var/log/nectarcollector/` | CDR log files + NATS server log |
| `/var/lib/nectarcollector/nats/` | JetStream persistent storage |

### Generated Config Example

The simple site config:
```json
{
  "hostname": "psna-ne-kearney-01",
  "fips_code": "1314010001",
  "vendor": "viper",
  "county": "kearney",
  "ports": ["ttyS1"],
  "dashboard_user": "psna",
  "dashboard_pass": "nectar2025"
}
```

Generates this full config at `/etc/nectarcollector/config.json`:
```json
{
  "app": {
    "name": "NectarCollector",
    "instance_id": "psna-ne-kearney-01",
    "fips_code": "1314010001"
  },
  "ports": [
    {
      "device": "/dev/ttyS1",
      "a_designation": "A1",
      "fips_code": "1314010001",
      "vendor": "viper",
      "county": "kearney",
      "baud_rate": 0,
      "enabled": true,
      "description": "CDR feed"
    }
  ],
  "detection": {
    "baud_rates": [9600, 19200, 38400, 57600, 115200, 4800, 2400, 1200, 300],
    "detection_timeout_sec": 5,
    "min_bytes_for_valid": 50
  },
  "nats": {
    "url": "nats://localhost:4222",
    "subject_prefix": "ne.cdr",
    "max_reconnects": -1,
    "reconnect_wait_sec": 5
  },
  "logging": {
    "base_path": "/var/log/nectarcollector",
    "max_size_mb": 50,
    "max_backups": 10,
    "compress": true,
    "level": "info"
  },
  "monitoring": {
    "port": 8080,
    "username": "psna",
    "password": "nectar2025"
  },
  "recovery": {
    "reconnect_delay_sec": 5,
    "max_reconnect_delay_sec": 300,
    "exponential_backoff": true
  }
}
```

---

## Security Notes

1. **Change default passwords** - Never use `CHANGE_ME` in production
2. **Tailscale-only SSH** - No public SSH server exposed
3. **Serial console fallback** - COM1/ttyS0 @ 115200 for emergency access
4. **Basic auth on dashboard** - Always set credentials
5. **NATS localhost only** - Binds to `127.0.0.1:4222`, not accessible from network
6. **Dashboard on 0.0.0.0:8080** - Protected by basic auth, consider binding to Tailscale IP only for sensitive deployments
7. **Service hardening** - Both services run with `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`
8. **Minimal users** - `nectarcollector` and `nats` are nologin service accounts

---

## Appendix: CDR Header Format

Each captured line is prefixed with:
```
[FIPSCODE][A1-16][YYYY-MM-DD HH:MM:SS.mmm]<original CDR data>
```

Example:
```
[1314010001][A1][2025-12-08 19:45:23.456]CALL START 555-1234...
```

This format allows:
- Deduplication across multiple collectors
- Time synchronization verification
- Source identification in aggregated streams

---

*Last updated: 2025-12-08*
