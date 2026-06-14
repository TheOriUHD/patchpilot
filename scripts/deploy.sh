#!/usr/bin/env bash
# patchpilot deploy helper
# Usage: ./scripts/deploy.sh "commit message"
#   --no-bump        commit & push but don't bump version or deploy
#   --no-deploy      bump version, commit, push, build — but don't touch LXC
#   --skip-tests     skip 'make check' before building (faster, less safe)
#   --major          bump major version (1.0.0 → 2.0.0)
#   --minor          bump minor (0.2.5 → 0.3.0)
#   default          bump patch (0.2.5 → 0.2.6)

set -euo pipefail

# ───────────────────────── config ─────────────────────────
LXC_HOST="${PATCHPILOT_LXC_HOST:-deploy@192.168.10.219}"
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

# ───────────────────────── colors ─────────────────────────
red() { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
yellow() { printf '\033[33m%s\033[0m\n' "$*"; }
cyan() { printf '\033[36m%s\033[0m\n' "$*"; }
bold() { printf '\033[1m%s\033[0m\n' "$*"; }

# ───────────────────────── args ───────────────────────────
COMMIT_MSG=""
BUMP_KIND="patch"
DO_BUMP=true
DO_DEPLOY=true
DO_TESTS=true

while [[ $# -gt 0 ]]; do
  case "$1" in
  --no-bump)
    DO_BUMP=false
    shift
    ;;
  --no-deploy)
    DO_DEPLOY=false
    shift
    ;;
  --skip-tests)
    DO_TESTS=false
    shift
    ;;
  --major)
    BUMP_KIND="major"
    shift
    ;;
  --minor)
    BUMP_KIND="minor"
    shift
    ;;
  --patch)
    BUMP_KIND="patch"
    shift
    ;;
  -h | --help)
    sed -n '2,9p' "$0"
    exit 0
    ;;
  -*)
    red "Unknown flag: $1"
    exit 1
    ;;
  *)
    if [[ -z "$COMMIT_MSG" ]]; then
      COMMIT_MSG="$1"
    else
      COMMIT_MSG="$COMMIT_MSG $1"
    fi
    shift
    ;;
  esac
done

if [[ -z "$COMMIT_MSG" ]]; then
  red "Usage: $(basename "$0") \"commit message\" [flags]"
  sed -n '2,9p' "$0"
  exit 1
fi

# ───────────────────────── preflight ──────────────────────
bold "patchpilot deploy"
cyan "  repo:        $REPO_ROOT"
cyan "  lxc:         $LXC_HOST"
cyan "  message:     $COMMIT_MSG"

# Find last tag (default v0.0.0 if none exists)
LAST_TAG="$(git tag -l 'v*' --sort=-v:refname | head -1)"
LAST_TAG="${LAST_TAG:-v0.0.0}"
cyan "  last tag:    $LAST_TAG"

# Parse version components
VERSION="${LAST_TAG#v}"
IFS='.' read -r MAJOR MINOR PATCH <<<"$VERSION"

if $DO_BUMP; then
  case "$BUMP_KIND" in
  major)
    MAJOR=$((MAJOR + 1))
    MINOR=0
    PATCH=0
    ;;
  minor)
    MINOR=$((MINOR + 1))
    PATCH=0
    ;;
  patch) PATCH=$((PATCH + 1)) ;;
  esac
  NEW_VERSION="v${MAJOR}.${MINOR}.${PATCH}"
  cyan "  new tag:     $NEW_VERSION ($BUMP_KIND bump)"
else
  NEW_VERSION="$LAST_TAG"
  cyan "  new tag:     (no bump)"
fi

echo

# ───────────────────────── tests ──────────────────────────
if $DO_TESTS; then
  yellow "→ running tests"
  if ! make check; then
    red "✗ tests failed — aborting"
    exit 1
  fi
  green "✓ tests passed"
  echo
fi

# ───────────────────────── commit & tag ───────────────────
yellow "→ committing"
if git diff --quiet && git diff --cached --quiet; then
  yellow "  nothing to commit — using existing HEAD"
else
  git add -A
  git commit -m "$COMMIT_MSG"
  green "✓ commit created"
fi

if $DO_BUMP; then
  yellow "→ tagging $NEW_VERSION"
  git tag -a "$NEW_VERSION" -m "$COMMIT_MSG"
  green "✓ tag $NEW_VERSION created"
fi

# ───────────────────────── push ───────────────────────────
yellow "→ pushing to origin"
git push
if $DO_BUMP; then
  git push --tags
fi
green "✓ pushed"
echo

if ! $DO_DEPLOY; then
  yellow "→ --no-deploy: stopping here"
  exit 0
fi

# ───────────────────────── build ──────────────────────────
yellow "→ building release tarball ($NEW_VERSION)"
make release VERSION="$NEW_VERSION"

TARBALL="dist/release/patchpilot-${NEW_VERSION}-linux-amd64.tar.gz"
if [[ ! -f "$TARBALL" ]]; then
  red "✗ expected tarball not found at $TARBALL"
  ls -la dist/release/
  exit 1
fi
green "✓ tarball built: $TARBALL"
echo

# ───────────────────────── deploy ─────────────────────────
yellow "→ uploading to $LXC_HOST"
yellow "  (you'll be prompted for the password)"
scp "$TARBALL" "$LXC_HOST:/tmp/"
green "✓ uploaded"
echo

# Compute expected md5 of the binary inside the tarball so we can verify
EXPECTED_MD5="$(tar -xzOf "$TARBALL" "patchpilot-${NEW_VERSION}-linux-amd64/patchpilot" | md5sum | awk '{print $1}')"
cyan "  expected binary md5: $EXPECTED_MD5"

yellow "→ installing on LXC"
yellow "  (you'll be prompted for sudo password)"

# Heredoc trick: pass expected md5 via env var, run all steps in one ssh session
ssh -t "$LXC_HOST" "EXPECTED_MD5='$EXPECTED_MD5' VERSION='$NEW_VERSION' bash" <<'REMOTE_EOF'
set -euo pipefail

TARBALL_NAME="patchpilot-${VERSION}-linux-amd64.tar.gz"
EXTRACT_DIR="/tmp/patchpilot-${VERSION}-linux-amd64"

cd /tmp
rm -rf "$EXTRACT_DIR"
tar -xzf "$TARBALL_NAME"
cd "$EXTRACT_DIR"

sudo systemctl stop patchpilot
sudo install -m 0755 -o root -g root patchpilot /usr/local/bin/patchpilot
sudo systemctl daemon-reload
sudo systemctl start patchpilot

# Wait for daemon to actually start (give it 3 seconds)
sleep 1
for i in 1 2 3; do
    if systemctl is-active --quiet patchpilot; then break; fi
    sleep 1
done

# Verify md5
ACTUAL_MD5="$(md5sum /usr/local/bin/patchpilot | awk '{print $1}')"
if [[ "$ACTUAL_MD5" != "$EXPECTED_MD5" ]]; then
    echo "✗ md5 mismatch! deployed binary doesn't match tarball"
    echo "  expected: $EXPECTED_MD5"
    echo "  actual:   $ACTUAL_MD5"
    exit 1
fi

# Show status
if systemctl is-active --quiet patchpilot; then
    echo "✓ daemon running, md5 verified: $ACTUAL_MD5"
else
    echo "✗ daemon failed to start — check: journalctl -u patchpilot -n 50"
    exit 1
fi
REMOTE_EOF

echo
green "════════════════════════════════════════"
green "  ✓ deployed $NEW_VERSION"
green "════════════════════════════════════════"
echo
cyan "next: ssh $LXC_HOST 'patchpilot'"
