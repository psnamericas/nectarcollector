# NectarCollector Ansible Deployment

Ansible playbooks for deploying and managing NectarCollector infrastructure.

## Quick Start

```bash
cd Ansible

# Deploy everything (initial setup)
ansible-playbook playbooks/deploy-all.yml

# Deploy code changes only (daily use)
ansible-playbook playbooks/06-app-deploy.yml
```

## Playbook Structure

| Playbook | Purpose | When to Use |
|----------|---------|-------------|
| `deploy-all.yml` | Master playbook, runs all others | Initial setup, full refresh |
| `01-system.yml` | Timezone, packages, serial console | System changes |
| `02-users.yml` | Service accounts (nats, nectarcollector) | User changes |
| `03-directories.yml` | Required directories | Directory changes |
| `04-nats.yml` | NATS server, CLI, config, JetStream | NATS changes |
| `05-app-config.yml` | NectarCollector config + service file | Config changes |
| `06-app-deploy.yml` | Build + deploy binary | **Code changes (daily use)** |

## Common Workflows

### Deploy New Code (Most Common)
After making code changes, push the new binary:
```bash
ansible-playbook playbooks/06-app-deploy.yml
```

### Update Configuration Only
Changed a site config file:
```bash
ansible-playbook playbooks/05-app-config.yml
```

### Full Deployment
Initial server setup or complete refresh:
```bash
ansible-playbook playbooks/deploy-all.yml
```

### Testing Individual Components
Run specific playbooks to test sections in isolation:
```bash
ansible-playbook playbooks/01-system.yml    # Just system setup
ansible-playbook playbooks/04-nats.yml      # Just NATS
```

### Skip Sections During Testing
Edit `deploy-all.yml` and comment out lines:
```yaml
- import_playbook: 01-system.yml
- import_playbook: 02-users.yml
# - import_playbook: 03-directories.yml    # <-- Skipped
- import_playbook: 04-nats.yml
# - import_playbook: 05-app-config.yml     # <-- Skipped
- import_playbook: 06-app-deploy.yml
```

## Inventory Management

Inventory is in `inventory/hosts` (YAML format).

### Adding a New Server

```yaml
general_servers:
  hosts:
    new-server.tailc90ef2.ts.net:
      ansible_host: new-server.tailc90ef2.ts.net
      ansible_user: root
      ansible_become: true
      site_config: setup-psna-ne-location-name-01.json
```

**Required Variables:**
- `ansible_host`: Tailscale DNS name or IP
- `ansible_user`: SSH user (typically root)
- `ansible_become`: Enable privilege escalation
- `site_config`: Filename from `deploy/configs/sites/` for this host

### Site Config Files
Each server needs a config file in `deploy/configs/sites/`:
```
deploy/configs/sites/
  setup-psna-ne-metro-omaha-01.json
  setup-psna-ne-northeast-norfolk-01.json
  ...
```

## Pre-Flight Checks

### Verify Connectivity
```bash
# Ping all hosts
ansible all -m ping

# Ping specific host
ansible psna-dev-nectarcollector-01.tailc90ef2.ts.net -m ping
```

### Dry Run (Check Mode)
Preview changes without applying:
```bash
ansible-playbook playbooks/deploy-all.yml --check

# With diff output
ansible-playbook playbooks/deploy-all.yml --check --diff
```

### Syntax Check
Validate playbook syntax:
```bash
ansible-playbook playbooks/deploy-all.yml --syntax-check
```

### List Hosts
Show which hosts would be targeted:
```bash
ansible-playbook playbooks/deploy-all.yml --list-hosts
```

## Security (Ansible Vault)

Sensitive data is encrypted with Ansible Vault.

- **Vault Password**: Stored in `.vault_pass` (not committed)
- **Encrypted Variables**: `group_vars/all/passwd.yml`

### Update Encrypted Password
```bash
ansible-vault encrypt_string --vault-password-file .vault_pass 'new_password' --name 'variable_name'
```

### Run with Vault Password
```bash
ansible-playbook playbooks/deploy-all.yml --ask-vault-pass
```

## Troubleshooting

### Verbose Output
Add `-v` flags for more detail:
```bash
ansible-playbook playbooks/06-app-deploy.yml -v      # Verbose
ansible-playbook playbooks/06-app-deploy.yml -vv     # More verbose
ansible-playbook playbooks/06-app-deploy.yml -vvv    # Debug level
```

### Run on Single Host
Limit execution to one host:
```bash
ansible-playbook playbooks/06-app-deploy.yml --limit psna-dev-nectarcollector-01.tailc90ef2.ts.net
```

### Step Through Tasks
Run interactively, confirming each task:
```bash
ansible-playbook playbooks/06-app-deploy.yml --step
```

### Start at Specific Task
Resume from a specific task:
```bash
ansible-playbook playbooks/06-app-deploy.yml --start-at-task="Copy binary to remote"
```

## Directory Structure

```
Ansible/
├── ansible.cfg              # Ansible configuration
├── inventory/
│   └── hosts                # Server inventory (YAML)
├── group_vars/
│   └── all/
│       └── passwd.yml       # Encrypted credentials
├── playbooks/
│   ├── deploy-all.yml       # Master playbook
│   ├── 01-system.yml        # System configuration
│   ├── 02-users.yml         # Service users
│   ├── 03-directories.yml   # Required directories
│   ├── 04-nats.yml          # NATS server setup
│   ├── 05-app-config.yml    # App configuration
│   ├── 06-app-deploy.yml    # Binary deployment
│   └── common.yml           # Legacy SNMP config
├── roles/
│   └── snmp/                # SNMP configuration role
└── README.md
```

## Requirements

- Ansible 2.9+
- Go installed locally (for building binary)
- Tailscale connected (for reaching remote hosts)
- SSH access to target servers
