#!/bin/sh
set -eu

# Only rewrite distribution repositories; preserve NVIDIA's CUDA repository.
mirror=${1%/}
[ -n "$mirror" ] || exit 0
for source in /etc/apt/sources.list /etc/apt/sources.list.d/*.list /etc/apt/sources.list.d/*.sources; do
    [ -f "$source" ] || continue
    sed -i -E \
        -e "s@https?://(archive.ubuntu.com|security.ubuntu.com)/ubuntu@${mirror}/ubuntu@g" \
        -e "s@https?://(deb.debian.org|security.debian.org)/debian-security@${mirror}/debian-security@g" \
        -e "s@https?://deb.debian.org/debian@${mirror}/debian@g" \
        "$source"
done
