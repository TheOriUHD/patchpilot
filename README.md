# patchpilot

patchpilot is a TUI-first homelab update orchestrator. This workspace currently contains the Milestone 1 skeleton: one Go binary with `tui` and `daemon` modes, embedded SQLite migrations, a Unix-socket daemon health API, and a fullscreen Bubble Tea shell.

## Development

```sh
make build
make run-daemon
make run-tui
```

On Linux the daemon defaults to `/run/patchpilot/patchpilot.sock`. On macOS and other development platforms it defaults to `~/.patchpilot/patchpilot.sock`. Override with `PATCHPILOT_SOCKET_PATH` when needed.
