#!/usr/bin/env bash
# setup.sh — Clone all sibling repos and build the agent sandbox system.
#
# Run from CommandGrid. Clones GhostProxy, RootFS, ToolCore, FlowSpec, JudgementD, api-gateway.
# Then builds GhostProxy, RootFS, control-plane. No temp files, no credential prompts.
#
# Layout after setup:
#   parent/
#   ├── CommandGrid/
#   ├── GhostProxy/
#   ├── RootFS/
#   ├── ToolCore/
#   ├── FlowSpec/
#   ├── JudgementD/
#   └── api-gateway/

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

log()  { echo -e "${GREEN}>>>${NC} $*"; }
warn() { echo -e "${YELLOW}>>>${NC} $*"; }
fail() { echo -e "${RED}>>> FATAL:${NC} $*"; exit 1; }
step() { echo -e "\n${CYAN}${BOLD}--- $* ---${NC}\n"; }

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PARENT_DIR="$(dirname "$SCRIPT_DIR")"

GHOSTPROXY_DIR="$PARENT_DIR/GhostProxy"
ROOTFS_DIR="$PARENT_DIR/RootFS"

# ─── Clone all repos ─────────────────────────────────────────────────────────

step "Cloning repos"

ORIGIN_URL=$(git -C "$SCRIPT_DIR" remote get-url origin 2>/dev/null || true)
if [[ -z "$ORIGIN_URL" ]]; then
    fail "Not a git repo or no origin. Clone repos manually."
fi

if [[ "$ORIGIN_URL" =~ git@github\.com:([^/]+)/ ]]; then
    ORG="${BASH_REMATCH[1]}"
    BASE="git@github.com:$ORG"
elif [[ "$ORIGIN_URL" =~ https?://[^/]*github\.com/([^/]+)/ ]]; then
    ORG="${BASH_REMATCH[1]}"
    BASE="https://github.com/$ORG"
else
    fail "Could not parse org from origin. Clone repos manually."
fi

# Repo name on GitHub -> local directory
clone_repo() { [[ -d "$2" ]] || { log "Cloning $2..."; git clone "$BASE/$1.git" "$2"; }; }
clone_repo "llm-proxy"      "$PARENT_DIR/GhostProxy"
clone_repo "sandbox-image"  "$PARENT_DIR/RootFS"
clone_repo "ToolCore"      "$PARENT_DIR/ToolCore"
clone_repo "FlowSpec"      "$PARENT_DIR/FlowSpec"
clone_repo "JudgementD"    "$PARENT_DIR/JudgementD"
clone_repo "api-gateway"   "$PARENT_DIR/api-gateway"

log "All repos ready"

# ─── Prerequisites ────────────────────────────────────────────────────────────

step "Checking prerequisites"

command -v go &>/dev/null || fail "Go not installed."
command -v docker &>/dev/null || fail "Docker not installed."
docker info &>/dev/null 2>&1 || fail "Docker daemon not running."

log "Prerequisites OK"

# ─── Build ────────────────────────────────────────────────────────────────────

step "Building GhostProxy"
cd "$GHOSTPROXY_DIR" && make build
log "Built GhostProxy"

step "Building RootFS image"
cd "$ROOTFS_DIR" && make image-local
log "Built rootfs image"

step "Building control-plane"
cd "$SCRIPT_DIR" && make build
log "Built control-plane"

# ─── Done ─────────────────────────────────────────────────────────────────────

step "Setup complete"

echo -e "
${BOLD}Built:${NC} GhostProxy, RootFS image, control-plane

${BOLD}Test it:${NC}
  cd $SCRIPT_DIR/examples/hello-world
  $SCRIPT_DIR/build/control-plane run
"
