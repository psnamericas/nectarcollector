#!/bin/bash
#===============================================================================
#
#         _
#        /_/_      .'''.
#     =O(_)))) ~=='  _  '=~    NECTAR
#        \_\       /  \        COLLECTOR
#                  \__/
#                               "Gathering data, one byte at a time"
#
#   ███╗   ██╗███████╗ ██████╗████████╗ █████╗ ██████╗
#   ████╗  ██║██╔════╝██╔════╝╚══██╔══╝██╔══██╗██╔══██╗
#   ██╔██╗ ██║█████╗  ██║        ██║   ███████║██████╔╝
#   ██║╚██╗██║██╔══╝  ██║        ██║   ██╔══██║██╔══██╗
#   ██║ ╚████║███████╗╚██████╗   ██║   ██║  ██║██║  ██║
#   ╚═╝  ╚═══╝╚══════╝ ╚═════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝
#                    COLLECTOR SETUP
#
#   PSNA Ubuntu Server Initialization Script
#   For Maple Systems IPC2210A / IPC2115A Industrial PCs
#
#   Usage: sudo ./setup.sh [config.json]
#
#===============================================================================

set -euo pipefail

#-------------------------------------------------------------------------------
# Configuration
#-------------------------------------------------------------------------------

SCRIPT_VERSION="1.0.0"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_FILE="/var/log/nectarcollector-setup.log"
CONFIG_FILE="${1:-}"

# Auto-discover config if not specified
if [[ -z "$CONFIG_FILE" ]]; then
    for candidate in \
        "${SCRIPT_DIR}/setup-config.json" \
        "${SCRIPT_DIR}/config.json" \
        "${SCRIPT_DIR}/../configs/setup-*.json" \
        "/opt/nectarcollector/setup-config.json"; do
        # Use first match (glob expands, take first)
        for f in $candidate; do
            if [[ -f "$f" ]]; then
                CONFIG_FILE="$f"
                break 2
            fi
        done
    done
fi

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# Software versions
GO_VERSION="1.23.4"
NATS_VERSION="2.10.22"
NATS_CLI_VERSION="0.1.5"

#-------------------------------------------------------------------------------
# Utility Functions
#-------------------------------------------------------------------------------

log() {
    local level="$1"
    shift
    local msg="$*"
    local timestamp
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo "[$timestamp] [$level] $msg" >> "$LOG_FILE"

    case "$level" in
        INFO)  echo -e "${CYAN}~^~${NC} $msg" ;;
        OK)    echo -e "${GREEN}*${NC} $msg" ;;
        WARN)  echo -e "${YELLOW}!${NC} $msg" ;;
        ERROR) echo -e "${RED}x${NC} $msg" ;;
        PHASE) echo -e "\n${BOLD}${YELLOW}    __         ${NC}"
               echo -e "${BOLD}${YELLOW}   /_/_   ${BLUE}$msg${NC}"
               echo -e "${BOLD}${YELLOW}=O(_))))  ${NC}${CYAN}~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~${NC}\n" ;;
    esac
}

die() {
    log ERROR "$1"
    exit 1
}

confirm() {
    local prompt="$1"
    local response
    echo -en "${YELLOW}?${NC} ${prompt} [y/N]: "
    read -r response
    [[ "$response" =~ ^[Yy]$ ]]
}

prompt() {
    local prompt="$1"
    local default="${2:-}"
    local response
    if [[ -n "$default" ]]; then
        echo -en "${YELLOW}?${NC} ${prompt} [${default}]: " >&2
        read -r response </dev/tty
        echo "${response:-$default}"
    else
        echo -en "${YELLOW}?${NC} ${prompt}: " >&2
        read -r response </dev/tty
        echo "$response"
    fi
}

prompt_password() {
    local prompt="$1"
    local password
    local confirm
    while true; do
        echo -en "${YELLOW}?${NC} ${prompt} (input hidden): " >&2
        IFS= read -rs password </dev/tty
        echo >&2
        echo -en "${YELLOW}?${NC} Confirm password: " >&2
        IFS= read -rs confirm </dev/tty
        echo >&2
        if [[ "$password" == "$confirm" ]]; then
            if [[ ${#password} -ge 12 ]]; then
                echo "$password"
                return 0
            else
                log WARN "Password must be at least 12 characters"
            fi
        else
            log WARN "Passwords do not match"
        fi
    done
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        die "This script must be run as root"
    fi
}

#-------------------------------------------------------------------------------
# Phase 0: Pre-flight Checks
#-------------------------------------------------------------------------------

preflight_checks() {
    log PHASE "PHASE 0: Pre-flight Checks"

    check_root
    log OK "Running as root"

    # Check Ubuntu
    if [[ -f /etc/os-release ]]; then
        . /etc/os-release
        if [[ "$ID" != "ubuntu" ]]; then
            die "This script requires Ubuntu (found: $ID)"
        fi
        log OK "Ubuntu $VERSION_ID detected"
    else
        die "Cannot determine OS"
    fi

    # Check internet connectivity
    if ping -c 1 -W 5 8.8.8.8 &>/dev/null; then
        log OK "Internet connectivity verified"
    else
        die "No internet connectivity"
    fi

    # Initialize log file
    mkdir -p "$(dirname "$LOG_FILE")"
    echo "=== NectarCollector Setup Log ===" > "$LOG_FILE"
    echo "Started: $(date)" >> "$LOG_FILE"
    echo "Version: $SCRIPT_VERSION" >> "$LOG_FILE"
    log OK "Log file initialized: $LOG_FILE"
}

#-------------------------------------------------------------------------------
# Phase 1: System Identity
#-------------------------------------------------------------------------------

phase_identity() {
    log PHASE "PHASE 1: System Identity"

    local hostname
    local fips_code

    # Load from config or prompt
    if [[ -n "$CONFIG_FILE" ]] && [[ -f "$CONFIG_FILE" ]]; then
        hostname=$(jq -r '.hostname // empty' "$CONFIG_FILE")
        fips_code=$(jq -r '.fips_code // empty' "$CONFIG_FILE")
        log INFO "Loaded identity from config file"
    fi

    # Prompt for missing values
    hostname="${hostname:-$(prompt "Hostname (e.g., psna-ne-lancaster-01)")}"
    fips_code="${fips_code:-$(prompt "Primary FIPS code (e.g., 3110900001)")}"

    # Set hostname
    log INFO "Setting hostname to: $hostname"
    hostnamectl set-hostname "$hostname"

    # Update /etc/hosts
    log INFO "Updating /etc/hosts"
    if grep -q "127.0.1.1" /etc/hosts; then
        sed -i "s/127.0.1.1.*/127.0.1.1\t$hostname/" /etc/hosts
    else
        echo "127.0.1.1	$hostname" >> /etc/hosts
    fi

    # Store for later phases (instance_id = hostname)
    echo "$hostname" > /tmp/.nc_instance_id
    echo "$fips_code" > /tmp/.nc_fips_code

    log OK "Hostname set to: $hostname"
}

#-------------------------------------------------------------------------------
# Phase 2: User Setup
#-------------------------------------------------------------------------------

phase_users() {
    log PHASE "PHASE 2: User Setup"

    # Create psna user if doesn't exist
    if id "psna" &>/dev/null; then
        log OK "User 'psna' already exists"
        if confirm "Reset password for 'psna' user?"; then
            local password
            password=$(prompt_password "Enter new password for psna user")
            echo "psna:$password" | chpasswd
            log OK "Password updated for 'psna'"
        else
            log INFO "Keeping existing password"
        fi
    else
        log INFO "Creating user 'psna'"
        useradd -m -s /bin/bash -G sudo,dialout psna
        log OK "User 'psna' created"

        # Set password for new user
        log INFO "Set password for 'psna' user"
        local password
        password=$(prompt_password "Enter password for psna user")
        echo "psna:$password" | chpasswd
        log OK "Password set for 'psna'"
    fi

    # Create nectarcollector service user
    if id "nectarcollector" &>/dev/null; then
        log OK "Service user 'nectarcollector' already exists"
    else
        log INFO "Creating service user 'nectarcollector'"
        useradd -r -s /usr/sbin/nologin -G dialout nectarcollector
        log OK "Service user 'nectarcollector' created"
    fi

    # Create nats service user
    if id "nats" &>/dev/null; then
        log OK "Service user 'nats' already exists"
    else
        log INFO "Creating service user 'nats'"
        useradd -r -s /usr/sbin/nologin nats
        log OK "Service user 'nats' created"
    fi
}

#-------------------------------------------------------------------------------
# Phase 3: Tailscale Setup
#-------------------------------------------------------------------------------

phase_tailscale() {
    log PHASE "PHASE 3: Tailscale Setup"

    # Install Tailscale
    if command -v tailscale &>/dev/null; then
        log OK "Tailscale already installed"
    else
        log INFO "Installing Tailscale..."
        curl -fsSL https://tailscale.com/install.sh | sh
        log OK "Tailscale installed"
    fi

    # Check if already connected
    if tailscale status &>/dev/null; then
        local ts_ip
        ts_ip=$(tailscale ip -4 2>/dev/null || echo "unknown")
        log OK "Tailscale already connected: $ts_ip"
    else
        # Authenticate
        log INFO "Starting Tailscale authentication"
        echo
        echo -e "${BOLD}${CYAN}┌────────────────────────────────────────────────────────────┐${NC}"
        echo -e "${BOLD}${CYAN}│  TAILSCALE AUTHENTICATION                                  │${NC}"
        echo -e "${BOLD}${CYAN}│                                                            │${NC}"
        echo -e "${BOLD}${CYAN}│  A browser link will appear below.                         │${NC}"
        echo -e "${BOLD}${CYAN}│  Open it on another device to authenticate this machine.   │${NC}"
        echo -e "${BOLD}${CYAN}└────────────────────────────────────────────────────────────┘${NC}"
        echo

        tailscale up --ssh --hostname="$(hostname)"

        # Verify connection
        sleep 3
        if tailscale status &>/dev/null; then
            local ts_ip
            ts_ip=$(tailscale ip -4 2>/dev/null || echo "unknown")
            log OK "Tailscale connected: $ts_ip"
        else
            die "Tailscale failed to connect"
        fi

        # Test Tailscale SSH
        log INFO "Tailscale SSH is enabled"
        echo
        echo -e "${BOLD}${GREEN}┌────────────────────────────────────────────────────────────┐${NC}"
        echo -e "${BOLD}${GREEN}│  TAILSCALE SSH READY                                       │${NC}"
        echo -e "${BOLD}${GREEN}│                                                            │${NC}"
        echo -e "${BOLD}${GREEN}│  You can now SSH via Tailscale:                            │${NC}"
        echo -e "${BOLD}${GREEN}│  ssh psna@$(hostname)                                      ${NC}"
        echo -e "${BOLD}${GREEN}│                                                            │${NC}"
        echo -e "${BOLD}${GREEN}│  Test this connection NOW before proceeding!               │${NC}"
        echo -e "${BOLD}${GREEN}└────────────────────────────────────────────────────────────┘${NC}"
        echo

        if ! confirm "Have you verified Tailscale SSH access works?"; then
            die "Please verify Tailscale SSH before continuing"
        fi
    fi
}

#-------------------------------------------------------------------------------
# Phase 4: SSH Lockdown
#-------------------------------------------------------------------------------

phase_ssh_lockdown() {
    log PHASE "PHASE 4: SSH Lockdown"

    # Check if OpenSSH is installed
    if [[ -f /etc/ssh/sshd_config ]]; then
        # Traditional OpenSSH is installed - lock it down
        log INFO "OpenSSH detected, hardening configuration"

        # Backup original config
        cp /etc/ssh/sshd_config /etc/ssh/sshd_config.backup.$(date +%Y%m%d)
        log OK "Backed up sshd_config"

        # Disable root SSH login
        sed -i 's/^#*PermitRootLogin.*/PermitRootLogin no/' /etc/ssh/sshd_config

        # Disable password auth (Tailscale SSH handles auth)
        sed -i 's/^#*PasswordAuthentication.*/PasswordAuthentication no/' /etc/ssh/sshd_config

        # Only listen on Tailscale interface (optional extra hardening)
        # This prevents SSH access from public internet entirely
        local ts_ip
        ts_ip=$(tailscale ip -4 2>/dev/null || echo "")
        if [[ -n "$ts_ip" ]]; then
            if ! grep -q "^ListenAddress" /etc/ssh/sshd_config; then
                echo "ListenAddress $ts_ip" >> /etc/ssh/sshd_config
                log OK "SSH restricted to Tailscale IP only ($ts_ip)"
            fi
        fi

        # Restart SSH
        systemctl restart sshd
        log OK "OpenSSH hardened"
    else
        # No OpenSSH - using Tailscale SSH only (cleanest setup!)
        log OK "No OpenSSH installed - using Tailscale SSH exclusively"
        log OK "This is the recommended secure configuration"
    fi

    # Ensure root can still login via serial console (emergency access)
    if [[ -f /etc/securetty ]]; then
        if ! grep -q "ttyS0" /etc/securetty; then
            echo "ttyS0" >> /etc/securetty
            log OK "Root login enabled on serial console (ttyS0)"
        fi
    fi
}

#-------------------------------------------------------------------------------
# Phase 5: System Configuration
#-------------------------------------------------------------------------------

phase_system() {
    log PHASE "PHASE 5: System Configuration"

    # Set timezone
    log INFO "Setting timezone to UTC"
    timedatectl set-timezone UTC
    log OK "Timezone set to UTC"

    # Update system
    log INFO "Updating system packages (this may take a while)..."
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get upgrade -y -qq
    log OK "System packages updated"

    # Install dependencies
    log INFO "Installing dependencies..."
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq \
        curl \
        wget \
        git \
        jq \
        htop \
        iotop \
        net-tools \
        unzip \
        build-essential
    log OK "Dependencies installed"

    # Configure serial console
    log INFO "Configuring serial console on COM1 (ttyS0)"

    # Update GRUB for serial console
    cat > /etc/default/grub.d/serial-console.cfg << 'EOF'
# Serial console configuration for recovery access
GRUB_TERMINAL="console serial"
GRUB_SERIAL_COMMAND="serial --speed=115200 --unit=0 --word=8 --parity=no --stop=1"
GRUB_CMDLINE_LINUX="console=tty0 console=ttyS0,115200n8"
EOF

    update-grub
    log OK "GRUB configured for serial console"

    # Enable getty on serial port
    systemctl enable serial-getty@ttyS0.service
    log OK "Serial getty enabled on ttyS0"
}

#-------------------------------------------------------------------------------
# Phase 6: Install Go
#-------------------------------------------------------------------------------

phase_go() {
    log PHASE "PHASE 6: Install Go $GO_VERSION"

    # Check if correct version already installed
    if command -v /usr/local/go/bin/go &>/dev/null; then
        local current_version
        current_version=$(/usr/local/go/bin/go version | awk '{print $3}' | sed 's/go//')
        if [[ "$current_version" == "$GO_VERSION" ]]; then
            log OK "Go $GO_VERSION already installed"
            # Ensure PATH is set
            export PATH=$PATH:/usr/local/go/bin
            return 0
        fi
        log INFO "Upgrading Go from $current_version to $GO_VERSION"
    fi

    log INFO "Downloading Go $GO_VERSION..."
    local arch
    arch=$(dpkg --print-architecture)
    case "$arch" in
        amd64) arch="amd64" ;;
        arm64) arch="arm64" ;;
        *) die "Unsupported architecture: $arch" ;;
    esac

    wget -q "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" -O /tmp/go.tar.gz

    # Remove old installation
    rm -rf /usr/local/go

    # Extract new version
    tar -C /usr/local -xzf /tmp/go.tar.gz
    rm /tmp/go.tar.gz

    # Add to PATH for all users
    cat > /etc/profile.d/go.sh << 'EOF'
export PATH=$PATH:/usr/local/go/bin
export GOPATH=$HOME/go
export PATH=$PATH:$GOPATH/bin
EOF

    # Source for current session
    export PATH=$PATH:/usr/local/go/bin

    log OK "Go $GO_VERSION installed"
}

#-------------------------------------------------------------------------------
# Phase 7: Install NATS Server
#-------------------------------------------------------------------------------

phase_nats() {
    log PHASE "PHASE 7: Install NATS Server $NATS_VERSION"

    # Check if NATS already installed and running
    if command -v nats-server &>/dev/null && systemctl is-active --quiet nats-server 2>/dev/null; then
        log OK "NATS server already installed and running"
        return 0
    fi

    # Determine architecture once for all downloads
    local arch
    arch=$(dpkg --print-architecture)

    # Download NATS server if not present
    if [[ ! -f /usr/local/bin/nats-server ]]; then
        log INFO "Downloading NATS server..."

        wget -q "https://github.com/nats-io/nats-server/releases/download/v${NATS_VERSION}/nats-server-v${NATS_VERSION}-linux-${arch}.tar.gz" -O /tmp/nats.tar.gz

        tar -xzf /tmp/nats.tar.gz -C /tmp
        mv "/tmp/nats-server-v${NATS_VERSION}-linux-${arch}/nats-server" /usr/local/bin/
        chmod +x /usr/local/bin/nats-server
        rm -rf /tmp/nats*

        log OK "NATS server binary installed"
    else
        log OK "NATS server binary already present"
    fi

    # Install NATS CLI
    if command -v nats &>/dev/null; then
        log OK "NATS CLI already installed"
    else
        log INFO "Installing NATS CLI v${NATS_CLI_VERSION}..."
        wget -q "https://github.com/nats-io/natscli/releases/download/v${NATS_CLI_VERSION}/nats-${NATS_CLI_VERSION}-linux-${arch}.zip" -O /tmp/nats-cli.zip
        unzip -q /tmp/nats-cli.zip -d /tmp
        install -m 755 "/tmp/nats-${NATS_CLI_VERSION}-linux-${arch}/nats" /usr/local/bin/nats
        rm -rf "/tmp/nats-${NATS_CLI_VERSION}-linux-${arch}" /tmp/nats-cli.zip
        log OK "NATS CLI installed"
    fi

    # Create directories (idempotent)
    mkdir -p /etc/nectarcollector
    mkdir -p /var/lib/nectarcollector/nats
    mkdir -p /var/log/nectarcollector
    chown -R nats:nats /var/lib/nectarcollector/nats
    # Log dir needs to be writable by both nats and nectarcollector users
    chown root:root /var/log/nectarcollector
    chmod 777 /var/log/nectarcollector

    # Create NATS config
    log INFO "Creating NATS configuration..."
    cat > /etc/nectarcollector/nats-server.conf << 'EOF'
# NATS Server Configuration for NectarCollector
# Secured: localhost only - no external network access

# Bind to localhost only for security
listen: 127.0.0.1:4222
server_name: nectarcollector

# Maximum payload (CDR lines are small)
max_payload: 64KB

# JetStream configuration
jetstream: {
    store_dir: "/var/lib/nectarcollector/nats"
    max_mem: 1GB
    max_file: 64GB
}

# Logging
debug: false
trace: false
logtime: true
log_file: "/var/log/nectarcollector/nats-server.log"
EOF

    log OK "NATS configuration created"

    # Install systemd service
    cat > /etc/systemd/system/nats-server.service << 'EOF'
[Unit]
Description=NATS Server for NectarCollector
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/nats-server -c /etc/nectarcollector/nats-server.conf
ExecReload=/bin/kill -HUP $MAINPID
Restart=always
RestartSec=5
User=nats
Group=nats
LimitNOFILE=65536

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/nectarcollector/nats /var/log/nectarcollector
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable nats-server
    systemctl start nats-server

    # Verify NATS is running
    sleep 2
    if systemctl is-active --quiet nats-server; then
        log OK "NATS server running"
    else
        log ERROR "NATS server failed to start"
        journalctl -u nats-server -n 20 --no-pager
        die "NATS server installation failed"
    fi

    # Create JetStream streams
    # Health stream: 5GB, 30-day TTL (small heartbeat messages)
    # CDR stream: 50GB, NO TTL (durable buffer until consumed)
    log INFO "Creating JetStream streams..."

    # Health stream
    if nats stream info health &>/dev/null; then
        log OK "Health stream already exists"
    else
        nats stream add health \
            --subjects "*.health.>" \
            --retention limits \
            --storage file \
            --max-age 30d \
            --max-bytes 5368709120 \
            --max-msg-size 8192 \
            --discard old \
            --replicas 1 \
            --dupe-window 1m \
            --no-deny-delete \
            --no-deny-purge \
            --defaults 2>&1 || true
        log OK "Health stream created (5GB, 30-day TTL)"
    fi

    # CDR stream - captures all CDR data on this box
    if nats stream info cdr &>/dev/null; then
        log OK "CDR stream already exists"
    else
        nats stream add cdr \
            --subjects "*.cdr.>" \
            --retention limits \
            --storage file \
            --max-bytes 53687091200 \
            --max-msg-size 8192 \
            --discard old \
            --replicas 1 \
            --dupe-window 2m \
            --no-deny-delete \
            --no-deny-purge \
            --defaults 2>&1 || true
        log OK "CDR stream created (50GB, NO TTL - durable)"
    fi

    # Events stream - discrete events like state changes, reconnects, errors
    # 1GB storage, no TTL (will store years of sparse event data)
    if nats stream info events &>/dev/null; then
        log OK "Events stream already exists"
    else
        nats stream add events \
            --subjects "*.events.>" \
            --retention limits \
            --storage file \
            --max-bytes 1073741824 \
            --max-msg-size 4096 \
            --discard old \
            --replicas 1 \
            --dupe-window 1m \
            --no-deny-delete \
            --no-deny-purge \
            --defaults 2>&1 || true
        log OK "Events stream created (1GB, NO TTL - sparse events)"
    fi
}

#-------------------------------------------------------------------------------
# Phase 8: Install NectarCollector
#-------------------------------------------------------------------------------

phase_nectarcollector() {
    log PHASE "PHASE 8: Install NectarCollector"

    # Check if already installed
    if [[ -f /usr/local/bin/nectarcollector ]]; then
        log OK "NectarCollector binary already installed"
    else
        # Check for pre-built binary
        local binary_path=""
        for path in \
            "${SCRIPT_DIR}/../nectarcollector" \
            "${SCRIPT_DIR}/../../nectarcollector" \
            "/tmp/nectarcollector" \
            "./nectarcollector"; do
            if [[ -f "$path" ]] && [[ -x "$path" ]]; then
                binary_path="$path"
                break
            fi
        done

        if [[ -n "$binary_path" ]]; then
            log INFO "Installing pre-built binary from: $binary_path"
            install -m 755 "$binary_path" /usr/local/bin/nectarcollector
        else
            log WARN "No pre-built binary found"
            if confirm "Build from source?"; then
                if [[ -d "${SCRIPT_DIR}/../.." ]] && [[ -f "${SCRIPT_DIR}/../../go.mod" ]]; then
                    log INFO "Building from source..."
                    cd "${SCRIPT_DIR}/../.."
                    /usr/local/go/bin/go build -o /usr/local/bin/nectarcollector
                    log OK "Built from source"
                else
                    die "Source code not found"
                fi
            else
                log WARN "Skipping NectarCollector binary installation"
                return 0
            fi
        fi

        log OK "NectarCollector binary installed"
    fi

    # Create configuration
    phase_nectarcollector_config

    # Install systemd service
    cat > /etc/systemd/system/nectarcollector.service << 'EOF'
[Unit]
Description=NectarCollector Serial CDR Capture Service
After=network.target nats-server.service
Wants=nats-server.service

[Service]
Type=simple
ExecStart=/usr/local/bin/nectarcollector -config /etc/nectarcollector/config.json
Restart=always
RestartSec=5
User=nectarcollector
Group=nectarcollector
WorkingDirectory=/var/lib/nectarcollector

# Hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/nectarcollector /var/lib/nectarcollector
PrivateTmp=true

# Serial device access
SupplementaryGroups=dialout

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable nectarcollector

    # Start if config has enabled ports
    if jq -e '.ports[] | select(.enabled == true)' /etc/nectarcollector/config.json &>/dev/null; then
        log INFO "Config has enabled ports, starting NectarCollector..."
        systemctl start nectarcollector
        sleep 2
        if systemctl is-active --quiet nectarcollector; then
            log OK "NectarCollector running"
        else
            log WARN "NectarCollector failed to start - check config and serial ports"
            journalctl -u nectarcollector -n 10 --no-pager
        fi
    else
        log OK "NectarCollector service installed (not started - no enabled ports in config)"
    fi
}

# Generate full NectarCollector config from simple setup config
generate_config_from_simple() {
    local setup_config="$1"

    local hostname fips_code vendor county dashboard_user dashboard_pass
    hostname=$(jq -r '.hostname // ""' "$setup_config")
    fips_code=$(jq -r '.fips_code // "0000000000"' "$setup_config")
    vendor=$(jq -r '.vendor // "unknown"' "$setup_config")
    county=$(jq -r '.county // "unknown"' "$setup_config")
    dashboard_user=$(jq -r '.dashboard_user // ""' "$setup_config")
    dashboard_pass=$(jq -r '.dashboard_pass // ""' "$setup_config")

    # Get state prefix from hostname (psna-XX-...)
    local state_prefix
    state_prefix=$(echo "$hostname" | sed -n 's/psna-\([a-z]*\)-.*/\1/p')
    state_prefix="${state_prefix:-xx}"

    # Build ports array from simple list
    local ports_json="[]"
    local designation=1
    for port in $(jq -r '.ports[]' "$setup_config"); do
        # Add /dev/ prefix if not present
        [[ "$port" != /dev/* ]] && port="/dev/$port"

        local port_json
        port_json=$(jq -n \
            --arg dev "$port" \
            --arg a_des "A$designation" \
            --arg fips "$fips_code" \
            --arg vendor "$vendor" \
            --arg county "$county" \
            '{
                device: $dev,
                a_designation: $a_des,
                fips_code: $fips,
                vendor: $vendor,
                county: $county,
                baud_rate: 0,
                enabled: true,
                description: "CDR feed"
            }')
        ports_json=$(echo "$ports_json" | jq --argjson port "$port_json" '. += [$port]')
        ((designation++))
    done

    # Generate full config
    cat > /etc/nectarcollector/config.json << EOF
{
  "app": {
    "name": "NectarCollector",
    "instance_id": "${hostname}",
    "fips_code": "${fips_code}"
  },
  "ports": ${ports_json},
  "detection": {
    "baud_rates": [9600, 19200, 38400, 57600, 115200, 4800, 2400, 1200, 300],
    "detection_timeout_sec": 5,
    "min_bytes_for_valid": 50
  },
  "nats": {
    "url": "nats://localhost:4222",
    "subject_prefix": "${state_prefix}.cdr",
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
    "username": "${dashboard_user}",
    "password": "${dashboard_pass}"
  },
  "recovery": {
    "reconnect_delay_sec": 5,
    "max_reconnect_delay_sec": 300,
    "exponential_backoff": true
  }
}
EOF

    log OK "Generated config with ${designation-1} port(s)"
}

phase_nectarcollector_config() {
    local instance_id fips_code
    instance_id=$(cat /tmp/.nc_instance_id 2>/dev/null || echo "")
    fips_code=$(cat /tmp/.nc_fips_code 2>/dev/null || echo "")

    # Check for config file
    if [[ -n "$CONFIG_FILE" ]] && [[ -f "$CONFIG_FILE" ]]; then
        # Check config format: simple (has "ports" array of strings) or full (has "nectarcollector_config" or "app")
        if jq -e '.ports[0] | type == "string"' "$CONFIG_FILE" &>/dev/null; then
            # Simple format - generate full config
            log INFO "Generating NectarCollector config from simple setup config"
            generate_config_from_simple "$CONFIG_FILE"
        elif jq -e '.nectarcollector_config' "$CONFIG_FILE" &>/dev/null; then
            # Legacy nested format
            log INFO "Extracting NectarCollector config from nested setup config"
            jq '.nectarcollector_config' "$CONFIG_FILE" > /etc/nectarcollector/config.json
        elif jq -e '.app' "$CONFIG_FILE" &>/dev/null; then
            # Direct full config
            log INFO "Installing full configuration from: $CONFIG_FILE"
            cp "$CONFIG_FILE" /etc/nectarcollector/config.json
        else
            log WARN "Unknown config format, treating as full config"
            cp "$CONFIG_FILE" /etc/nectarcollector/config.json
        fi

        # Update instance_id if needed
        if [[ -n "$instance_id" ]]; then
            jq --arg id "$instance_id" '.app.instance_id = $id' \
                /etc/nectarcollector/config.json > /tmp/config.tmp && \
                mv /tmp/config.tmp /etc/nectarcollector/config.json
        fi
    else
        log INFO "Creating default configuration"

        # Detect serial devices
        local serial_devices=()
        for dev in /dev/ttyUSB* /dev/ttyS[1-9]*; do
            [[ -e "$dev" ]] && serial_devices+=("$dev")
        done

        # Build ports array
        local ports_json="[]"
        local designation=1
        for dev in "${serial_devices[@]}"; do
            # Skip ttyS0 (reserved for console)
            [[ "$dev" == "/dev/ttyS0" ]] && continue

            local port_json
            port_json=$(jq -n \
                --arg dev "$dev" \
                --arg a_des "A$designation" \
                --arg fips "${fips_code:-0000000000}" \
                '{
                    device: $dev,
                    a_designation: $a_des,
                    fips_code: $fips,
                    vendor: "unknown",
                    county: "unknown",
                    baud_rate: 0,
                    enabled: false,
                    description: "Unconfigured port"
                }')
            ports_json=$(echo "$ports_json" | jq --argjson port "$port_json" '. += [$port]')
            ((designation++))
        done

        # Get state prefix from hostname
        local state_prefix
        state_prefix=$(hostname | sed -n 's/psna-\([a-z]*\)-.*/\1/p')
        state_prefix="${state_prefix:-xx}"

        # Create config
        cat > /etc/nectarcollector/config.json << EOF
{
  "app": {
    "name": "NectarCollector",
    "instance_id": "${instance_id:-$(hostname)}",
    "fips_code": "${fips_code:-0000000000}"
  },
  "ports": ${ports_json},
  "detection": {
    "baud_rates": [9600, 19200, 38400, 57600, 115200, 4800, 2400, 1200, 300],
    "detection_timeout_sec": 5,
    "min_bytes_for_valid": 50
  },
  "nats": {
    "url": "nats://localhost:4222",
    "subject_prefix": "${state_prefix}.cdr",
    "stream_name": "${state_prefix}-cdr",
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
    "port": 8080
  },
  "recovery": {
    "reconnect_delay_sec": 5,
    "max_reconnect_delay_sec": 300,
    "exponential_backoff": true
  }
}
EOF
    fi

    chown nectarcollector:nectarcollector /etc/nectarcollector/config.json
    chmod 640 /etc/nectarcollector/config.json
    log OK "Configuration created: /etc/nectarcollector/config.json"
}

#-------------------------------------------------------------------------------
# Phase 9: Network Documentation
#-------------------------------------------------------------------------------

phase_network_docs() {
    log PHASE "PHASE 9: Network Documentation"

    echo
    echo -e "${BOLD}${CYAN}"
    cat << 'EOF'
    ╔═══════════════════════════════════════════════════════════════╗
    ║                   NETWORK INTERFACES                          ║
    ╠═══════════════════════════════════════════════════════════════╣
EOF
    echo -e "${NC}"

    # Collect MAC addresses for summary
    local mac_summary=""

    # Get network interface info
    local iface
    for iface in $(ls /sys/class/net | grep -v lo | grep -v tailscale); do
        local mac ip state driver
        mac=$(cat "/sys/class/net/$iface/address" 2>/dev/null || echo "unknown")
        ip=$(ip -4 addr show "$iface" 2>/dev/null | grep -oP '(?<=inet\s)\d+(\.\d+){3}' | head -1 || echo "no ip")
        state=$(cat "/sys/class/net/$iface/operstate" 2>/dev/null || echo "unknown")
        driver=$(basename "$(readlink -f /sys/class/net/$iface/device/driver 2>/dev/null)" 2>/dev/null || echo "unknown")

        echo -e "${CYAN}    ║${NC}  ${BOLD}$iface${NC}"
        echo -e "${CYAN}    ║${NC}    ├─ MAC:    ${GREEN}$mac${NC}"
        echo -e "${CYAN}    ║${NC}    ├─ IP:     $ip"
        echo -e "${CYAN}    ║${NC}    ├─ State:  $state"
        echo -e "${CYAN}    ║${NC}    └─ Driver: $driver"
        echo -e "${CYAN}    ║${NC}"

        # Build MAC summary
        mac_summary="${mac_summary}${iface}: ${mac}\n"
    done

    # Tailscale info
    local ts_ip ts_hostname
    ts_ip=$(tailscale ip -4 2>/dev/null || echo "not connected")
    ts_hostname=$(tailscale status --json 2>/dev/null | jq -r '.Self.HostName // "unknown"')

    echo -e "${CYAN}    ║${NC}  ${BOLD}tailscale0${NC}"
    echo -e "${CYAN}    ║${NC}    ├─ IP:       ${GREEN}$ts_ip${NC}"
    echo -e "${CYAN}    ║${NC}    └─ Hostname: $ts_hostname"

    echo -e "${BOLD}${CYAN}"
    cat << 'EOF'
    ╚═══════════════════════════════════════════════════════════════╝
EOF
    echo -e "${NC}"

    # Print MAC address summary box for easy documentation
    echo
    echo -e "${BOLD}${GREEN}┌────────────────────────────────────────────────────────────┐${NC}"
    echo -e "${BOLD}${GREEN}│  MAC ADDRESSES - COPY FOR DOCUMENTATION                    │${NC}"
    echo -e "${BOLD}${GREEN}├────────────────────────────────────────────────────────────┤${NC}"
    echo -e "${BOLD}${GREEN}│${NC}  Hostname: ${BOLD}$(hostname)${NC}"
    for iface in $(ls /sys/class/net | grep -v lo | grep -v tailscale); do
        local mac
        mac=$(cat "/sys/class/net/$iface/address" 2>/dev/null || echo "unknown")
        printf "${BOLD}${GREEN}│${NC}  %-10s ${BOLD}%s${NC}\n" "$iface:" "$mac"
    done
    echo -e "${BOLD}${GREEN}│${NC}  Tailscale: ${BOLD}$ts_ip${NC}"
    echo -e "${BOLD}${GREEN}└────────────────────────────────────────────────────────────┘${NC}"
    echo

    # Save to file
    {
        echo "# Network Configuration"
        echo "# Generated: $(date)"
        echo "# Hostname: $(hostname)"
        echo ""
        echo "## MAC Addresses (for DHCP reservations / documentation)"
        for iface in $(ls /sys/class/net | grep -v lo | grep -v tailscale); do
            printf "%-12s %s\n" "$iface:" "$(cat /sys/class/net/$iface/address 2>/dev/null)"
        done
        echo ""
        echo "## Tailscale"
        echo "IP: $ts_ip"
        echo "Hostname: $ts_hostname"
        echo ""
        echo "## Full Interface Details"
        ip addr show
    } > /etc/nectarcollector/network-info.txt

    log OK "Network info saved to /etc/nectarcollector/network-info.txt"
}

#-------------------------------------------------------------------------------
# Phase 10: Serial Port Documentation
#-------------------------------------------------------------------------------

phase_serial_docs() {
    log PHASE "PHASE 10: Serial Port Documentation"

    echo
    echo -e "${BOLD}${CYAN}"
    cat << 'EOF'
    ╔═══════════════════════════════════════════════════════════════╗
    ║                     SERIAL PORTS                              ║
    ╠═══════════════════════════════════════════════════════════════╣
EOF
    echo -e "${NC}"

    # Built-in serial ports
    for i in 0 1 2 3; do
        local dev="/dev/ttyS$i"
        if [[ -e "$dev" ]]; then
            local status="available"
            if [[ $i -eq 0 ]]; then
                status="${YELLOW}console${NC}"
            fi
            echo -e "${CYAN}    ║${NC}  ${BOLD}ttyS$i${NC} (COM$((i+1))) - $status"
        fi
    done

    echo -e "${CYAN}    ║${NC}"

    # USB serial adapters
    local usb_count=0
    for dev in /dev/ttyUSB*; do
        if [[ -e "$dev" ]]; then
            local devname
            devname=$(basename "$dev")
            local usb_path
            usb_path=$(udevadm info -q path -n "$dev" 2>/dev/null | grep -oP 'usb\d+/[^/]+' | head -1 || echo "unknown")
            echo -e "${CYAN}    ║${NC}  ${BOLD}$devname${NC} - USB adapter ($usb_path)"
            ((usb_count++))
        fi
    done

    if [[ $usb_count -eq 0 ]]; then
        echo -e "${CYAN}    ║${NC}  ${YELLOW}No USB serial adapters detected${NC}"
    fi

    echo -e "${BOLD}${CYAN}"
    cat << 'EOF'
    ╚═══════════════════════════════════════════════════════════════╝
EOF
    echo -e "${NC}"

    echo -e "${CYAN}    ℹ${NC}  COM1 (ttyS0) is reserved for emergency console access"
    echo -e "${CYAN}    ℹ${NC}  To connect via serial console from another machine:"
    echo -e "${CYAN}    ℹ${NC}    Linux:  ${BOLD}screen /dev/ttyUSB0 115200${NC}"
    echo -e "${CYAN}    ℹ${NC}    macOS:  ${BOLD}screen /dev/cu.usbserial-* 115200${NC}"
    echo
}

#-------------------------------------------------------------------------------
# Phase 11: Final Summary
#-------------------------------------------------------------------------------

phase_summary() {
    log PHASE "PHASE 11: The Hive is Ready"

    echo -e "${BOLD}${YELLOW}"
    cat << 'EOF'

           _
          /_/_      .'''.                    THE HIVE IS READY
       =O(_)))) ~=='  _  '=~
          \_\       /  \     Your NectarCollector is buzzing!
                    \__/

    ╔═══════════════════════════════════════════════════════════════╗
    ║         __    __    __    __    __    __    __    __          ║
    ║       /   \ /   \ /   \ /   \ /   \ /   \ /   \ /   \         ║
    ║      | H  | E  | X  | A  | G  | O  | N  | S  |         ║
    ║       \___/ \___/ \___/ \___/ \___/ \___/ \___/ \___/         ║
    ║                                                               ║
    ║         Honeycomb storage initialized and ready               ║
    ║                                                               ║
    ╚═══════════════════════════════════════════════════════════════╝

EOF
    echo -e "${NC}"

    local ts_ip
    ts_ip=$(tailscale ip -4 2>/dev/null || echo "unknown")

    echo -e "${BOLD}Pathways to the Hive:${NC}"
    echo -e "  ${YELLOW}~^~${NC} Tailscale SSH:  ssh psna@$(hostname)"
    echo -e "  ${YELLOW}~^~${NC} Tailscale IP:   $ts_ip"
    echo -e "  ${YELLOW}~^~${NC} Network SSH:    ssh psna@<local-ip> (fallback)"
    echo -e "  ${YELLOW}~^~${NC} Serial Console: COM1/ttyS0 @ 115200 8N1"
    echo -e "  ${YELLOW}~^~${NC} Physical:       HDMI/DP + USB keyboard"
    echo

    echo -e "${BOLD}Worker Bees (Services):${NC}"
    local nats_status nc_status
    nats_status=$(systemctl is-active nats-server 2>/dev/null || echo "unknown")
    nc_status=$(systemctl is-enabled nectarcollector 2>/dev/null || echo "unknown")
    echo -e "  ${GREEN}*${NC} NATS Server:     $nats_status"
    echo -e "  ${YELLOW}*${NC} NectarCollector: $nc_status (awaiting port config)"
    echo

    echo -e "${BOLD}Honeycomb Cells (Config):${NC}"
    echo -e "  ${CYAN}<>${NC} /etc/nectarcollector/config.json"
    echo -e "  ${CYAN}<>${NC} /etc/nectarcollector/nats-server.conf"
    echo

    echo -e "${BOLD}Next Steps - Time to Pollinate:${NC}"
    echo -e "  1. Edit /etc/nectarcollector/config.json"
    echo -e "     - Configure serial ports (device, vendor, county, fips)"
    echo -e "     - Set enabled: true for active ports"
    echo -e "  2. Start the collector: ${BOLD}systemctl start nectarcollector${NC}"
    echo -e "  3. Check the swarm: ${BOLD}systemctl status nectarcollector${NC}"
    echo -e "  4. View HoneyView dashboard: ${BOLD}http://$(hostname):8080${NC}"
    echo

    echo -e "${BOLD}Flight Logs:${NC}"
    echo -e "  ${CYAN}<>${NC} Setup log:   $LOG_FILE"
    echo -e "  ${CYAN}<>${NC} NATS log:    /var/log/nectarcollector/nats-server.log"
    echo -e "  ${CYAN}<>${NC} CDR nectar:  /var/log/nectarcollector/*.log"
    echo

    log OK "The hive is ready! Happy collecting!"
    echo "Completed: $(date)" >> "$LOG_FILE"

    # Offer to reboot
    echo
    if confirm "Reboot now to apply all changes (recommended)?"; then
        log INFO "Rebooting..."
        reboot
    else
        log WARN "Remember to reboot before putting into production"
    fi
}

#-------------------------------------------------------------------------------
# Main
#-------------------------------------------------------------------------------

main() {
    clear
    echo -e "${BOLD}${YELLOW}"
    cat << 'EOF'

             _
            /_/_      .'''.
         =O(_)))) ~=='  _  '=~      "Gathering data, one byte at a time"
            \_\       /  \
                      \__/
EOF
    echo -e "${NC}"
    echo -e "${BOLD}${CYAN}"
    cat << 'EOF'
    ███╗   ██╗███████╗ ██████╗████████╗ █████╗ ██████╗
    ████╗  ██║██╔════╝██╔════╝╚══██╔══╝██╔══██╗██╔══██╗
    ██╔██╗ ██║█████╗  ██║        ██║   ███████║██████╔╝
    ██║╚██╗██║██╔══╝  ██║        ██║   ██╔══██║██╔══██╗
    ██║ ╚████║███████╗╚██████╗   ██║   ██║  ██║██║  ██║
    ╚═╝  ╚═══╝╚══════╝ ╚═════╝   ╚═╝   ╚═╝  ╚═╝╚═╝  ╚═╝
                       COLLECTOR
EOF
    echo -e "${NC}"
    echo -e "    ${CYAN}PSNA Ubuntu Server Setup Script v${SCRIPT_VERSION}${NC}"
    echo -e "    ${CYAN}For Maple Systems IPC2210A / IPC2115A${NC}"
    echo

    if [[ -n "$CONFIG_FILE" ]]; then
        if [[ -f "$CONFIG_FILE" ]]; then
            echo -e "    ${GREEN}▶${NC} Using config: $CONFIG_FILE"
        else
            die "Config file not found: $CONFIG_FILE"
        fi
    else
        echo -e "    ${YELLOW}▶${NC} No config file found (interactive mode)"
        echo -e "    ${CYAN}ℹ${NC}  Place setup-config.json in same directory to auto-load"
    fi
    echo

    if ! confirm "Ready to begin setup?"; then
        echo "Aborted."
        exit 0
    fi

    preflight_checks
    phase_identity
    phase_users
    phase_tailscale
    phase_ssh_lockdown
    phase_system
    phase_go
    phase_nats
    phase_nectarcollector
    phase_network_docs
    phase_serial_docs
    phase_summary
}

main "$@"
