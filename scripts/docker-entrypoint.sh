#!/bin/bash
set -e

# ─── Fix ownership of bind-mounted directories ───
# When users bind-mount host directories (e.g. ./skills/preloaded),
# the mount inherits the host UID/GID which may differ from the
# container's appuser. This entrypoint runs as root, fixes ownership,
# then drops privileges to appuser via gosu — the same pattern used
# by official postgres/redis images.

# Directories that may be bind-mounted and need appuser access
MOUNT_DIRS=(
    /app/skills/preloaded
    /data/files
)

for dir in "${MOUNT_DIRS[@]}"; do
    if [ -d "$dir" ]; then
        chown -R appuser:appuser "$dir" 2>/dev/null || true
    fi
done

# Match the mounted Docker socket's group so appuser can start external
# Docker-runtime plugins without running the application as root.
if [ -S /var/run/docker.sock ]; then
    DOCKER_SOCKET_GID="$(stat -c '%g' /var/run/docker.sock)"
    if ! getent group "$DOCKER_SOCKET_GID" >/dev/null 2>&1; then
        groupadd -g "$DOCKER_SOCKET_GID" weknora-docker 2>/dev/null || true
    fi
    DOCKER_GROUP="$(getent group "$DOCKER_SOCKET_GID" | cut -d: -f1)"
    if [ -n "$DOCKER_GROUP" ]; then
        usermod -aG "$DOCKER_GROUP" appuser
    fi
fi

# ─── Merge built-in skills into preloaded ───
# Built-in skills are backed up at /app/skills/_builtin during image build.
# After a bind-mount replaces /app/skills/preloaded, copy back any
# missing built-in skills (without overwriting user-provided ones).
BUILTIN_DIR="/app/skills/_builtin"
PRELOADED_DIR="/app/skills/preloaded"

if [ -d "$BUILTIN_DIR" ]; then
    mkdir -p "$PRELOADED_DIR"
    for skill_dir in "$BUILTIN_DIR"/*/; do
        [ -d "$skill_dir" ] || continue
        skill_name="$(basename "$skill_dir")"
        if [ ! -d "$PRELOADED_DIR/$skill_name" ]; then
            cp -r "$skill_dir" "$PRELOADED_DIR/$skill_name"
        fi
    done
    chown -R appuser:appuser "$PRELOADED_DIR"
fi

# ─── Drop privileges and exec the main process ───
exec gosu appuser "$@"
