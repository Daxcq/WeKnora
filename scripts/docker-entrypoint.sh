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
# Docker-runtime plugins without weakening the host socket permissions.
grant_docker_sock_to_appuser() {
    local sock="$1" gid grp
    [ -S "$sock" ] || return 0
    if gosu appuser sh -c "test -r \"$sock\" && test -w \"$sock\"" 2>/dev/null; then
        return 0
    fi
    gid="$(stat -c '%g' "$sock" 2>/dev/null || true)"
    if [ -z "$gid" ] || [ "$gid" = "0" ]; then
        echo "weknora: Docker socket is not group-writable for appuser; Docker plugins may be unavailable" >&2
        return 0
    fi
    if ! getent group "$gid" >/dev/null 2>&1; then
        groupadd -g "$gid" weknora-docker >/dev/null 2>&1 || true
    fi
    grp="$(getent group "$gid" | cut -d: -f1)"
    [ -n "$grp" ] && usermod -aG "$grp" appuser >/dev/null 2>&1 || true
}

grant_docker_sock_to_appuser /var/run/docker.sock
case "${DOCKER_HOST:-}" in
    unix://*) grant_docker_sock_to_appuser "${DOCKER_HOST#unix://}" ;;
esac

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
export HOME=/home/appuser
exec gosu appuser "$@"
