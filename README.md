# patchpilot

patchpilot is a TUI-first homelab update orchestrator for running package updates across Linux VMs and LXCs, Proxmox-adjacent hosts, TrueNAS SCALE, and Home Assistant OS from one local daemon. It is built for operators who want a quiet terminal workflow: inventory, schedules, live run logs, audit trails, key rotation, and backup/restore without turning package updates into a web platform.

## Quick Start: Debian 12

Install patchpilot on a Debian 12 machine or LXC that can SSH to the systems you want to manage:

```sh
tar -xzf patchpilot-dev-linux-amd64.tar.gz
cd patchpilot-dev-linux-amd64
sudo ./install.sh
sudo systemctl start patchpilot
patchpilot tui
```

The installer prints the orchestrator public key. Add that key to `authorized_keys` for the `patchpilot` account on every Standard Linux host you want to manage.

In the TUI, press `a` on the Hosts tab to add a host with the Standard wizard. Then open the Schedules tab, press `a`, choose a frequency, select hosts, and save the schedule.

## Install

Release tarballs contain:

- `patchpilot`, the binary
- `install.sh`, the Linux installer
- `patchpilot.service`, for Linux packages only
- `README.md`
- `LICENSE`
- `patchpilot.sha256`, the binary checksum

On Debian 12:

```sh
tar -xzf patchpilot-dev-linux-amd64.tar.gz
cd patchpilot-dev-linux-amd64
shasum -a 256 -c patchpilot.sha256
sudo ./install.sh
sudo systemctl start patchpilot
```

The installer is idempotent. Re-running it updates the binary and systemd unit, preserves existing config and secrets, and only restarts the service if it was already running.

Production paths:

```text
/etc/patchpilot/config.yaml
/etc/patchpilot/secret.key
/var/lib/patchpilot/patchpilot.db
/var/lib/patchpilot/ssh/id_ed25519
/var/log/patchpilot/daemon.log
/run/patchpilot/patchpilot.sock
```

## Configuration

The default config is written to `/etc/patchpilot/config.yaml` on first install:

```yaml
paths:
  data_dir: /var/lib/patchpilot
  database_path: /var/lib/patchpilot/patchpilot.db
  socket_path: /run/patchpilot/patchpilot.sock
  log_path: /var/log/patchpilot/daemon.log
  secret_key_path: /etc/patchpilot/secret.key
```

Environment overrides are available for development and unusual deployments:

```text
PATCHPILOT_CONFIG
PATCHPILOT_DATA_DIR
PATCHPILOT_DB_PATH
PATCHPILOT_SOCKET_PATH
PATCHPILOT_LOG_PATH
PATCHPILOT_SECRET_KEY_PATH
```

The daemon reads the full config file. TUI and CLI clients only need the daemon socket and do not read `/etc/patchpilot/config.yaml`; set `PATCHPILOT_SOCKET` to override the client socket path. Without that override, clients use `/run/patchpilot/patchpilot.sock` on Linux and `~/.patchpilot/patchpilot.sock` on macOS/development systems.

## Add Hosts

Open the TUI:

```sh
patchpilot tui
```

### Standard Linux

Use the Standard wizard from the Hosts tab. The wizard connects with an initial password or key, creates or verifies a `patchpilot` remote account, installs the orchestrator public key, detects the package manager, and prepares the least-privilege sudoers entry for package updates.

Supported Standard package managers include Debian/Ubuntu `apt`, Arch `pacman`, Alpine `apk`, Fedora `dnf`, and compatible Linux hosts where the detected command set is available.

### TrueNAS SCALE

Add a TrueNAS host with a SCALE API token. patchpilot stores the encrypted token locally and uses the TrueNAS API to check and apply system updates. TrueNAS Core is intentionally refused because the supported API surface is SCALE-specific.

### Home Assistant OS

Add a HAOS host with a Home Assistant long-lived access token. patchpilot uses the Home Assistant `/api/hassio` proxy to call Supervisor and Core update endpoints, applying Supervisor updates before Core updates.

## Schedules

Schedules live on the Schedules tab. A schedule has a frequency, time fields, enabled state, and assigned hosts. The scheduler prevents overlapping scheduled runs; if another run is active, the scheduled fire is skipped and recorded.

Experimental concurrency is off by default. When enabled in Settings, schedules expose `Max` concurrency from 1 to 10. Only Group A Standard non-Proxmox hosts run in parallel. TrueNAS, HAOS, Proxmox hosts, and the self-update host always run sequentially.

## Manual Runs

From Hosts, press `R` on a host to start a manual update run. From Schedules, press `R` to run a schedule immediately. Manual and CLI runs queue behind an active run; scheduled runs skip instead of queuing.

The live run view streams host status and log output. patchpilot records run history and host logs so old runs can be inspected later from the Logs tab.

## Monitoring

The Logs tab shows run history with filters for time range, status, and trigger. Open a run to view persisted logs without subscribing to a live stream.

Useful service commands:

```sh
sudo systemctl status patchpilot
sudo journalctl -u patchpilot -f
sudo tail -f /var/log/patchpilot/daemon.log
```

The systemd unit restarts on failure, waits 5 seconds between attempts, and stops retrying after 3 crashes within 60 seconds. That leaves a clear failed service state instead of an infinite crash loop.

## Key Rotation

The Settings tab shows the orchestrator public key and can rotate the keypair. Rotation is all-or-nothing: patchpilot installs and verifies the new key on every enabled Standard host before replacing the local key. If any host rejects the new key, patchpilot attempts to remove the new key from hosts where it was installed and keeps the old local key.

After rotation, patchpilot removes the old key from managed hosts. Hosts that are offline or unreachable should be fixed before retrying rotation.

## Backup And Restore

The Settings tab can export a YAML inventory backup with `format_version: 1`. Backups include hosts, schedules, host links, and the concurrency setting. API tokens are not exported.

Import detects name conflicts before changing inventory. For conflicts, choose skip, overwrite, or rename. Overwriting host metadata preserves existing API token secrets because imported backups do not contain tokens.

## How It Works

patchpilot is one binary with two everyday modes:

- `patchpilot daemon` runs the local orchestrator, SQLite store, scheduler, Unix-socket API, run executor, and log streamer.
- `patchpilot tui` connects to the local daemon socket and provides the operator interface.

The daemon owns persistent state in SQLite, runs migrations at startup, serializes update runs, and writes run logs under `/var/log/patchpilot/runs`. Standard Linux hosts are managed over SSH with a dedicated orchestrator key. TrueNAS SCALE and HAOS hosts use their native HTTP APIs with encrypted local token storage.

## Security Model

The local `patchpilot` system user runs the daemon and owns `/var/lib/patchpilot` and `/var/log/patchpilot`. It can read `/etc/patchpilot/secret.key` through the `patchpilot` group and can write only the whitelisted state, log, runtime, and config paths allowed by the hardened systemd unit.

The orchestrator SSH key controls access to Standard hosts where you install its public key. On those hosts, the remote `patchpilot` account should have only the package-manager sudo permissions generated by the wizard. Protect that key like production automation credentials.

The encrypted secret key protects API tokens stored in SQLite for TrueNAS SCALE and HAOS. If someone obtains both the database and `/etc/patchpilot/secret.key`, they can decrypt those API tokens.

## What It Does Not Do

- No automatic reboots.
- No notifications yet.
- No web UI yet.
- No Windows hosts.
- No macOS hosts.
- No package pinning or policy engine.
- No cross-cluster locking beyond the local daemon.

## Troubleshooting

### Daemon will not start

Run:

```sh
sudo systemctl status patchpilot
sudo journalctl -u patchpilot --no-pager -n 100
```

Check that `/etc/patchpilot/config.yaml` points at writable state/log paths and that `/etc/patchpilot/secret.key` is exactly 32 bytes.

### TUI cannot connect

Confirm the daemon is running and the socket exists:

```sh
sudo systemctl start patchpilot
ls -l /run/patchpilot/patchpilot.sock
```

If you are using development paths, make sure `PATCHPILOT_SOCKET_PATH` matches for both daemon and TUI.

### Host onboarding fails

Verify DNS/IP reachability, SSH port, username, and password/key. The initial account needs enough privilege to create the remote `patchpilot` account, install the public key, and write the sudoers entry.

### Managed host says permission denied

Re-add the orchestrator public key to the remote `patchpilot` user's `authorized_keys`. In the TUI, open Settings and copy the current public key.

### Package updates fail with sudo errors

The remote sudoers entry may be missing a command path. Re-run the Standard wizard for that host so patchpilot can detect command paths and install the package-manager-specific entry.

### Schedules do not fire when expected

Check that the schedule is enabled, hosts are assigned and enabled, and system time is correct on the daemon host. Time drift can make schedule previews and actual fire times confusing.

### Scheduled run was skipped

patchpilot skips scheduled runs when another run is active. This is expected and is recorded in the schedule's skip metadata. Manual runs queue; schedules do not.

### TrueNAS or HAOS authentication fails

Create a fresh API token, update the host in patchpilot, and confirm the URL points at the correct host. TrueNAS Core is not supported; use TrueNAS SCALE.

### Key rotation aborts

At least one enabled Standard host rejected the new key or became unreachable. Fix SSH access to every listed host and retry. patchpilot keeps the old local key when rotation aborts.

### Backup import reports conflicts

Choose skip, overwrite, or rename. Imports preflight conflicts before mutation, so it is safe to cancel and inspect the YAML first.

## Development

```sh
make test
make build
make run-daemon
make run-tui
```

Release packaging:

```sh
make release VERSION=v0.1.0
```

Issues: <https://github.com/TheOriUHD/patchpilot/issues>
