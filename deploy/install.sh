#!/usr/bin/env sh
set -eu

APP=patchpilot
USER_NAME=patchpilot
GROUP_NAME=patchpilot
BIN_PATH=/usr/local/bin/patchpilot
UNIT_PATH=/etc/systemd/system/patchpilot.service
ETC_DIR=/etc/patchpilot
DATA_DIR=/var/lib/patchpilot
LOG_DIR=/var/log/patchpilot
SSH_DIR=/var/lib/patchpilot/ssh
SSH_KEY=/var/lib/patchpilot/ssh/id_ed25519
SSH_PUB=/var/lib/patchpilot/ssh/id_ed25519.pub
CONFIG_PATH=/etc/patchpilot/config.yaml
SECRET_KEY=/etc/patchpilot/secret.key

TMP_SECRET=
TMP_CONFIG=

cleanup() {
    if [ -n "$TMP_SECRET" ]; then
        rm -f "$TMP_SECRET"
    fi
    if [ -n "$TMP_CONFIG" ]; then
        rm -f "$TMP_CONFIG"
    fi
}
trap cleanup EXIT

fail() {
    printf '%s install: %s\n' "$APP" "$*" >&2
    exit 1
}

need_cmd() {
    command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

run_as_patchpilot() {
    runuser -u "$USER_NAME" -- "$@"
}

warn_locale_if_needed() {
    case "${LANG:-}" in
        C|POSIX) ;;
        *) return ;;
    esac

    has_utf8_locale=0
    if [ -r /etc/locale.gen ]; then
        while IFS= read -r locale_line; do
            case "$locale_line" in
                \#*|'') continue ;;
                *UTF-8*|*utf-8*|*UTF8*|*utf8*) has_utf8_locale=1; break ;;
            esac
        done < /etc/locale.gen
    fi

    if [ "$has_utf8_locale" -eq 0 ]; then
        printf '\nWarning: Your system locale is set to C, which will cause TUI rendering issues. Run: sudo apt-get install -y locales && sudo dpkg-reconfigure locales (select en_US.UTF-8 or de_DE.UTF-8).\n'
    fi
}

if [ "$(id -u)" -ne 0 ]; then
    fail "run as root: sudo ./install.sh"
fi

if [ "$(uname -s)" != "Linux" ]; then
    fail "install.sh installs the Linux daemon only; macOS is supported for development runs, not production install"
fi

need_cmd chmod
need_cmd chown
need_cmd dd
need_cmd getent
need_cmd install
need_cmd md5sum
need_cmd runuser
need_cmd ssh-keygen
need_cmd systemctl
need_cmd wc

SCRIPT_DIR=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
BINARY_SRC=${PATCHPILOT_BINARY:-$SCRIPT_DIR/patchpilot}
UNIT_SRC=${PATCHPILOT_SERVICE:-$SCRIPT_DIR/patchpilot.service}

[ -f "$BINARY_SRC" ] || fail "binary not found: $BINARY_SRC"
[ -f "$UNIT_SRC" ] || fail "systemd unit not found: $UNIT_SRC"

binary_status=updated
binary_backup_status="none (no previous binary)"
config_status=preserved
secret_status=preserved
ssh_status=preserved
unit_status=updated

was_running=0
if systemctl is-active --quiet "$APP" 2>/dev/null; then
    was_running=1
fi

if getent group "$GROUP_NAME" >/dev/null 2>&1; then
    printf 'Group %s already exists; leaving it in place.\n' "$GROUP_NAME"
else
    if command -v groupadd >/dev/null 2>&1; then
        groupadd --system "$GROUP_NAME"
    elif command -v addgroup >/dev/null 2>&1; then
        addgroup --system "$GROUP_NAME"
    else
        fail "neither groupadd nor addgroup is available"
    fi
fi

if getent passwd "$USER_NAME" >/dev/null 2>&1; then
    printf 'User %s already exists; leaving it in place.\n' "$USER_NAME"
else
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --no-create-home --gid "$GROUP_NAME" --shell /usr/sbin/nologin "$USER_NAME"
    elif command -v adduser >/dev/null 2>&1; then
        adduser --system --no-create-home --ingroup "$GROUP_NAME" --disabled-login --disabled-password "$USER_NAME"
    else
        fail "neither useradd nor adduser is available"
    fi
fi

install -d -o root -g "$GROUP_NAME" -m 0750 "$ETC_DIR"
install -d -o "$USER_NAME" -g "$GROUP_NAME" -m 0700 "$DATA_DIR"
install -d -o "$USER_NAME" -g "$GROUP_NAME" -m 0750 "$LOG_DIR"
install -d -o "$USER_NAME" -g "$GROUP_NAME" -m 0700 "$SSH_DIR"

if [ -f "$BIN_PATH" ]; then
    install -m 0755 -o root -g root "$BIN_PATH" "$BIN_PATH.prev"
    binary_backup_status="$BIN_PATH.prev"
fi
install -m 0755 -o root -g root "$BINARY_SRC" "$BIN_PATH"
installed_md5=$(md5sum "$BIN_PATH")
installed_md5=${installed_md5%% *}

if [ ! -f "$SSH_KEY" ]; then
    ssh-keygen -t ed25519 -N '' -C "patchpilot@$(hostname 2>/dev/null || printf unknown)" -f "$SSH_KEY" >/dev/null
    chown "$USER_NAME:$GROUP_NAME" "$SSH_KEY" "$SSH_PUB"
    chmod 0600 "$SSH_KEY"
    chmod 0644 "$SSH_PUB"
    ssh_status=created
else
    printf 'SSH key already exists; leaving it in place.\n'
    chown "$USER_NAME:$GROUP_NAME" "$SSH_KEY"
    chmod 0600 "$SSH_KEY"
    if [ ! -f "$SSH_PUB" ]; then
        ssh-keygen -y -f "$SSH_KEY" > "$SSH_PUB"
        ssh_status="preserved private key; regenerated public key"
    fi
    chown "$USER_NAME:$GROUP_NAME" "$SSH_PUB"
    chmod 0644 "$SSH_PUB"
fi

if [ ! -f "$SECRET_KEY" ]; then
    TMP_SECRET=$(mktemp "$ETC_DIR/.secret.key.XXXXXX")
    dd if=/dev/urandom of="$TMP_SECRET" bs=32 count=1 status=none
    chown root:"$GROUP_NAME" "$TMP_SECRET"
    chmod 0640 "$TMP_SECRET"
    mv "$TMP_SECRET" "$SECRET_KEY"
    TMP_SECRET=
    secret_status=created
else
    printf 'Secret key already exists; leaving it in place.\n'
    size=$(wc -c < "$SECRET_KEY" | tr -d '[:space:]')
    [ "$size" = "32" ] || fail "$SECRET_KEY must contain exactly 32 bytes; found $size"
    chown root:"$GROUP_NAME" "$SECRET_KEY"
    chmod 0640 "$SECRET_KEY"
fi

if [ ! -f "$CONFIG_PATH" ]; then
    TMP_CONFIG=$(mktemp "$ETC_DIR/.config.yaml.XXXXXX")
    cat > "$TMP_CONFIG" <<'YAML'
paths:
  data_dir: /var/lib/patchpilot
  database_path: /var/lib/patchpilot/patchpilot.db
  socket_path: /run/patchpilot/patchpilot.sock
  log_path: /var/log/patchpilot/daemon.log
  secret_key_path: /etc/patchpilot/secret.key
YAML
    chown root:"$GROUP_NAME" "$TMP_CONFIG"
    chmod 0640 "$TMP_CONFIG"
    mv "$TMP_CONFIG" "$CONFIG_PATH"
    TMP_CONFIG=
    config_status=created
else
    printf 'Config already exists; leaving it in place.\n'
fi

install -o root -g root -m 0644 "$UNIT_SRC" "$UNIT_PATH"
systemctl daemon-reload
systemctl enable "$APP" >/dev/null

load_state=$(systemctl show "$APP" --property=LoadState --value 2>/dev/null || true)
[ "$load_state" = "loaded" ] || fail "systemd unit did not load cleanly (LoadState=$load_state)"
systemctl status "$APP" --no-pager >/dev/null 2>&1 || status_code=$?
status_code=${status_code:-0}
case "$status_code" in
    0|3) ;;
    *) fail "systemctl status returned unexpected exit code $status_code" ;;
esac

[ -x "$BIN_PATH" ] || fail "$BIN_PATH is not executable"
run_as_patchpilot test -w "$DATA_DIR" || fail "$DATA_DIR is not writable by $USER_NAME"
run_as_patchpilot test -w "$LOG_DIR" || fail "$LOG_DIR is not writable by $USER_NAME"
run_as_patchpilot test -r "$SECRET_KEY" || fail "$SECRET_KEY is not readable by $USER_NAME"

if [ "$was_running" -eq 1 ]; then
    systemctl restart "$APP"
    printf 'patchpilot was already running and has been restarted with the updated unit/binary.\n'
else
    printf 'patchpilot is installed. Start it with: sudo systemctl start patchpilot\n'
fi

warn_locale_if_needed

printf '\nInstall summary:\n'
printf '  binary: %s (%s, md5 %s)\n' "$binary_status" "$BIN_PATH" "$installed_md5"
printf '  previous binary backup: %s\n' "$binary_backup_status"
printf '  systemd unit: %s (%s)\n' "$unit_status" "$UNIT_PATH"
printf '  config: %s (%s)\n' "$config_status" "$CONFIG_PATH"
printf '  secret key: %s (%s)\n' "$secret_status" "$SECRET_KEY"
printf '  ssh keypair: %s (%s)\n' "$ssh_status" "$SSH_KEY"

printf '\nAdd this public key to authorized_keys on each host you want patchpilot to manage:\n\n'
cat "$SSH_PUB"
printf "\n\nTo use the patchpilot TUI as a non-root user, add yourself to the patchpilot group: sudo usermod -aG patchpilot \$USER (then log out and back in).\n"
