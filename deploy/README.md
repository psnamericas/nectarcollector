# NectarCollector Deployment Guide

```
         _
        /_/_      .'''.
     =O(_)))) ~=='  _  '=~    "Gathering data, one byte at a time"
        \_\       /  \
                  \__/
```

This guide covers deploying NectarCollector to Maple Systems industrial PCs for PSAP CDR/ALI serial data capture.

## Supported Hardware

| Model | CPU | Temp Range | Serial Ports | Notes |
|-------|-----|------------|--------------|-------|
| IPC2210A | Intel Atom x6425E | -40 to +70C | 4x RS-232 | Wide-temp, outdoor rated |
| IPC2115A | Intel N100 | 0 to +50C | 2x RS-232 | Compact, indoor use |

## Quick Start

### 1. Initial Access

SSH into the fresh Ubuntu box using the default credentials:

```bash
ssh root@<ip-address>
# Default password: (provided with hardware)
```

### 2. Transfer Files

From your workstation, copy the deployment files:

```bash
# Copy the entire deploy folder
scp -r deploy/ root@<ip-address>:/root/

# Or just the essentials
scp deploy/scripts/setup.sh root@<ip-address>:/root/
scp deploy/configs/example-site.json root@<ip-address>:/root/site.json
```

### 3. Run Setup

```bash
# Make executable and run
chmod +x /root/deploy/scripts/setup.sh

# Interactive mode (prompts for all values)
sudo /root/deploy/scripts/setup.sh

# Or with pre-configured site file
sudo /root/deploy/scripts/setup.sh /root/site.json
```

### 4. Follow the Prompts

The script will guide you through:

1. **System Identity** - Hostname, instance ID, FIPS code
2. **User Setup** - Create `psna` admin user with strong password
3. **Tailscale** - Install and authenticate (click the link!)
4. **SSH Lockdown** - Disable root SSH, keep password as fallback
5. **System Config** - Timezone, packages, serial console
6. **Go Installation** - For future builds
7. **NATS Server** - JetStream with 64GB local storage
8. **NectarCollector** - Binary and default config
9. **Documentation** - Network and serial port inventory

## Access Methods

After setup, you have multiple ways to access the box:

| Method | Command | When to Use |
|--------|---------|-------------|
| Tailscale SSH | `ssh psna@hostname` | Normal daily access |
| Network SSH | `ssh psna@<local-ip>` | Tailscale is down |
| Serial Console | `screen /dev/ttyUSB0 115200` | Network is down |
| Physical | HDMI + USB keyboard | Last resort |

### Serial Console Access

COM1 (ttyS0) is reserved for emergency console access:

```
┌─────────────────┐    Null Modem    ┌──────────────────┐
│  IPC Box        │    DB9 Cable     │  Your Laptop     │
│  COM1 (ttyS0)   │◄────────────────►│  USB-Serial      │
│  115200 8N1     │                  │  Adapter         │
└─────────────────┘                  └──────────────────┘
```

```bash
# Linux/macOS
screen /dev/ttyUSB0 115200
# or
picocom -b 115200 /dev/ttyUSB0

# Windows: PuTTY -> Serial -> COM3 -> 115200
```

Root login is allowed on serial console (physical access = root access).

## Configuration

### Site Configuration File

Create a JSON file for each site before deployment:

```json
{
  "hostname": "psna-ne-lancaster-01",
  "instance_id": "ne-lancaster-01",
  "fips_code": "3110900001",
  "ports": [
    {
      "device": "/dev/ttyUSB0",
      "a_designation": "A1",
      "fips_code": "3110900001",
      "vendor": "intrado",
      "county": "lancaster",
      "baud_rate": 0,
      "enabled": true,
      "description": "Lancaster County PSAP - VIPER primary"
    }
  ],
  "nats": {
    "subject_prefix": "ne.cdr",
    "stream_name": "ne-cdr"
  }
}
```

### Hostname Convention

```
psna-{state}-{region}-{sequence}

Examples:
  psna-ne-lancaster-01    # Nebraska, Lancaster County, box 1
  psna-ne-region24-01     # Nebraska, Region 24 multi-county
  psna-pa-allegheny-02    # Pennsylvania, Allegheny County, box 2
```

### Serial Port Allocation

| Port | Device | Purpose |
|------|--------|---------|
| COM1 | /dev/ttyS0 | **Reserved for console** |
| COM2 | /dev/ttyS1 | CDR capture |
| COM3 | /dev/ttyS2 | CDR capture |
| USB | /dev/ttyUSB* | Additional CDR capture |

### NATS Subject Format

Follows PEMA convention:

```
{state}.cdr.{vendor}.{county}.{fips}

Examples:
  ne.cdr.intrado.lancaster.3110900001
  ne.cdr.solacom.region24.3101700001
  pa.cdr.vesta.allegheny.4200300001
```

## Post-Installation

### Enable Serial Ports

Edit `/etc/nectarcollector/config.json`:

```bash
sudo nano /etc/nectarcollector/config.json
```

Set `enabled: true` for each port you want to capture.

### Start the Service

```bash
sudo systemctl start nectarcollector
sudo systemctl status nectarcollector
```

### View the Dashboard

Open in browser: `http://<hostname>:8080`

The HoneyView dashboard shows real-time port status and recent CDR data.

### Monitor Logs

```bash
# Service logs
journalctl -u nectarcollector -f

# CDR capture logs
tail -f /var/log/nectarcollector/*.log

# NATS logs
tail -f /var/log/nectarcollector/nats-server.log
```

## Troubleshooting

### Tailscale Won't Connect

```bash
# Check status
tailscale status

# Re-authenticate
sudo tailscale up --ssh
```

### Serial Port Permission Denied

```bash
# Verify user is in dialout group
groups nectarcollector

# Should include 'dialout'
# If not:
sudo usermod -aG dialout nectarcollector
sudo systemctl restart nectarcollector
```

### No Data From Serial Port

```bash
# Test with minicom
sudo apt install minicom
minicom -D /dev/ttyUSB0 -b 9600

# Check if port exists
ls -la /dev/ttyUSB* /dev/ttyS*
```

### NATS Not Running

```bash
sudo systemctl status nats-server
journalctl -u nats-server -n 50
```

## File Locations

| File | Purpose |
|------|---------|
| `/usr/local/bin/nectarcollector` | Main binary |
| `/usr/local/bin/nats-server` | NATS server binary |
| `/etc/nectarcollector/config.json` | NectarCollector config |
| `/etc/nectarcollector/nats-server.conf` | NATS config |
| `/var/lib/nectarcollector/nats/` | JetStream storage |
| `/var/log/nectarcollector/` | All logs |

## Directory Structure

```
deploy/
├── README.md                       # This file
├── scripts/
│   └── setup.sh                    # Main setup script
├── configs/
│   ├── example-site.json           # Site config template (copy & customize)
│   ├── example-nebraska.json       # Full Nebraska PSAP example
│   ├── nats-server.conf            # NATS JetStream server config
│   └── streams/
│       └── nebraska-cdr.json       # Stream definition example
└── services/
    ├── nectarcollector.service     # Systemd unit for collector
    └── nats-server.service         # Systemd unit for NATS
```

## Full Project Structure

```
nectarcollector/
│
├── main.go                         # Entry point
├── go.mod / go.sum                 # Dependencies
│
├── capture/                        # Serial capture logic
│   ├── channel.go                  # Per-port state machine
│   └── manager.go                  # Orchestrates all channels
│
├── config/                         # Configuration loading
│   ├── config.go                   # Structs and defaults
│   └── validate.go                 # Validation logic
│
├── serial/                         # Serial port abstraction
│   ├── reader.go                   # RealReader with hardening
│   └── detection.go                # Autobaud detection
│
├── output/                         # Data output
│   ├── dual.go                     # DualWriter (log + NATS)
│   └── header.go                   # CDR header formatting
│
├── monitoring/                     # HoneyView dashboard
│   ├── server.go                   # HTTP endpoints
│   └── dashboard.html              # Embedded UI
│
├── configs/                        # Development configs
│   └── dev-example.json            # Local testing config
│
├── deploy/                         # Production deployment
│   ├── README.md                   # Deployment docs
│   ├── scripts/setup.sh            # Server setup script
│   ├── configs/                    # Production configs
│   └── services/                   # Systemd units
│
├── CLAUDE.md                       # AI assistant context
├── README.md                       # Project overview
└── SERIAL_HARDENING.md             # Serial reliability audit
```

---

*NectarCollector - Gathering data, one byte at a time*
