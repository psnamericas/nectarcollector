# NectarCollector Ansible Deployment

Ansible playbooks for deploying and managing NectarCollector infrastructure.

## Quick Start

1. Connect to Tailscale VPN

2. Go to Bitwarden and grab the password from the Ansible entry

3. Make sure the site has the correct config in: `deploy/configs/sites`

4. Edit the `Ansible/inventory/hosts` file and add/edit the new server (follow the example)

5. Run the playbook on the new server:
   `ansible-playbook master_playbook.yml --ask-vault-pass --limit new-server.tailc90ef2.ts.net`

### Notes:
   * <u>Make sure to include the `--limit` option otherwise the playbooks will run on ALL servers in the inventory file!</u><br>

   * To deploy just the code changes (code changes don't require passwd): <br>
    `ansible-playbook playbooks/06-app-deploy.yml --limit new-server.tailc90ef2.ts.net`

<br>


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

<br>


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

<br>
### Site Config Files
Each server needs a config file in `deploy/configs/sites/`:

```
deploy/configs/sites/setup-psna-ne-metro-omaha-01.json
```
<br>

## Security (Ansible Vault)

Sensitive data is encrypted with Ansible Vault.

- **Vault Password**: Stored in `.vault_pass` (not committed)
- **Encrypted Variables**: `group_vars/all/passwd.yml`

<br>

### Update Encrypted Password
```bash
ansible-vault encrypt_string --vault-password-file .vault_pass 'new_password' --name 'variable_name'
```
<br>


### Run with Vault Password
```bash
ansible-playbook playbooks/deploy-all.yml --ask-vault-pass
```
<br>

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
<br>

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
