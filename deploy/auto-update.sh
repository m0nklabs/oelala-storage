#!/usr/bin/env bash
# oelala-storage auto-update script
# Periodically checks GitHub for new commits, builds, and deploys to all nodes.
#
# Runs on the build host (node 1 / ai-kvm2) which has Go installed.
# Remote nodes receive the pre-built binary via SCP.
#
# Usage: ./deploy/auto-update.sh [--force]
#   --force  Deploy even if no new commits detected

set -euo pipefail

# ─── Configuration ───────────────────────────────────────────────────────────
REPO_DIR="/home/flip/oelala-storage"
BINARY_NAME="oelala-storage"
BUILD_OUTPUT="${REPO_DIR}/bin/${BINARY_NAME}"
BRANCH="main"
LOG_TAG="oelala-storage-autoupdate"

# Remote nodes: "user@host:binary_path:service_name"
REMOTE_NODES=(
    "flip@192.168.1.62:/home/flip/oelala-storage/oelala-storage:oelala-storage"
)

# Local service name (empty string to skip local restart)
LOCAL_SERVICE="oelala-storage"

# ─── Helpers ─────────────────────────────────────────────────────────────────
log() { echo "$(date -u '+%Y-%m-%d %H:%M:%S UTC') [$LOG_TAG] $*"; }
die() { log "❌ FATAL: $*"; exit 1; }

# ─── Pre-flight checks ──────────────────────────────────────────────────────
[[ -d "${REPO_DIR}/.git" ]] || die "Not a git repo: ${REPO_DIR}"
command -v go &>/dev/null || die "Go not installed"
command -v ssh &>/dev/null || die "SSH not available"

cd "${REPO_DIR}"

FORCE=0
[[ "${1:-}" == "--force" ]] && FORCE=1

# ─── Check for updates ──────────────────────────────────────────────────────
log "🔍 Fetching from origin..."
git fetch origin "${BRANCH}" --quiet

LOCAL_HEAD=$(git rev-parse HEAD)
REMOTE_HEAD=$(git rev-parse "origin/${BRANCH}")

if [[ "${LOCAL_HEAD}" == "${REMOTE_HEAD}" ]] && [[ ${FORCE} -eq 0 ]]; then
    log "✅ Already up to date (${LOCAL_HEAD:0:8}). Nothing to do."
    exit 0
fi

if [[ ${FORCE} -eq 1 ]] && [[ "${LOCAL_HEAD}" == "${REMOTE_HEAD}" ]]; then
    log "⚠️  Force mode — deploying current version (${LOCAL_HEAD:0:8})"
else
    log "📦 New commits detected: ${LOCAL_HEAD:0:8} → ${REMOTE_HEAD:0:8}"
    COMMITS=$(git log --oneline "${LOCAL_HEAD}..origin/${BRANCH}" 2>/dev/null | head -10)
    log "   Changes:"
    while IFS= read -r line; do
        log "   • ${line}"
    done <<< "${COMMITS}"
fi

# ─── Pull & Build ───────────────────────────────────────────────────────────
log "⬇️  Pulling latest changes..."
git pull origin "${BRANCH}" --quiet

NEW_HEAD=$(git rev-parse HEAD)
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

log "🔨 Building ${BINARY_NAME} (${VERSION})..."
LDFLAGS="-X main.Version=${VERSION} -X main.BuildTime=${BUILD_TIME} -s -w"

# Build for linux/amd64 (all our nodes are x86_64)
GOOS=linux GOARCH=amd64 go build -ldflags "${LDFLAGS}" -o "${BUILD_OUTPUT}" ./cmd/oelala-storage

[[ -f "${BUILD_OUTPUT}" ]] || die "Build failed — binary not found: ${BUILD_OUTPUT}"
BUILD_SIZE=$(stat -c%s "${BUILD_OUTPUT}")
BUILD_HASH=$(md5sum "${BUILD_OUTPUT}" | awk '{print $1}')
log "✅ Build OK: ${BUILD_OUTPUT} ($(numfmt --to=iec ${BUILD_SIZE}), md5:${BUILD_HASH:0:12})"

# ─── Deploy to local ────────────────────────────────────────────────────────
if [[ -n "${LOCAL_SERVICE}" ]]; then
    log "🔄 Restarting local service: ${LOCAL_SERVICE}..."
    sudo systemctl restart "${LOCAL_SERVICE}"
    sleep 2
    if systemctl is-active --quiet "${LOCAL_SERVICE}"; then
        log "✅ Local ${LOCAL_SERVICE} restarted OK"
    else
        log "❌ Local ${LOCAL_SERVICE} failed to start!"
        journalctl -u "${LOCAL_SERVICE}" --no-pager -n 5
    fi
fi

# ─── Deploy to remote nodes ─────────────────────────────────────────────────
for node_spec in "${REMOTE_NODES[@]}"; do
    IFS=':' read -r SSH_TARGET REMOTE_PATH REMOTE_SERVICE <<< "${node_spec}"
    log "📤 Deploying to ${SSH_TARGET}..."

    # Upload new binary to temp location, then atomic move
    REMOTE_TMP="${REMOTE_PATH}.new"
    if scp -q "${BUILD_OUTPUT}" "${SSH_TARGET}:${REMOTE_TMP}"; then
        # Atomic replace + restart
        ssh "${SSH_TARGET}" "
            chmod +x '${REMOTE_TMP}' && \
            mv '${REMOTE_TMP}' '${REMOTE_PATH}' && \
            sudo systemctl restart '${REMOTE_SERVICE}'
        "
        sleep 2
        # Verify remote service is healthy
        REMOTE_HOST=$(echo "${SSH_TARGET}" | cut -d@ -f2)
        if ssh "${SSH_TARGET}" "systemctl is-active --quiet '${REMOTE_SERVICE}'"; then
            log "✅ ${SSH_TARGET} — ${REMOTE_SERVICE} restarted OK"
        else
            log "❌ ${SSH_TARGET} — ${REMOTE_SERVICE} failed to start!"
            ssh "${SSH_TARGET}" "journalctl -u '${REMOTE_SERVICE}' --no-pager -n 5"
        fi
    else
        log "❌ SCP failed to ${SSH_TARGET} — skipping"
    fi
done

# ─── Summary ─────────────────────────────────────────────────────────────────
log "🎉 Auto-update complete: ${VERSION} (${NEW_HEAD:0:8})"
