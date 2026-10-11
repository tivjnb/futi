#!/bin/sh
# Usage: backup.sh SRC DEST
# Packs SRC into DEST/<name>-<timestamp>.tar.gz.
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: backup.sh SRC DEST" >&2
	exit 2
fi

src=$1
dest=$2

if [ ! -d "$src" ]; then
	echo "backup: not a directory: $src" >&2
	exit 1
fi

mkdir -p -- "$dest"
name=$(basename -- "$(cd -- "$src" && pwd)")
archive="$dest/$name-$(date +%Y%m%d-%H%M%S).tar.gz"

tar -czf "$archive" -C "$src" .
echo "created $archive"
