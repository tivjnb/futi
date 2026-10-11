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
archive="$name-$(date +%Y%m%d-%H%M%S).tar.gz"

# Архив собираем вне src: dest может лежать внутри src.
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT

tar -czf "$tmp/$archive" -C "$src" .
mv -- "$tmp/$archive" "$dest/$archive"
echo "created $dest/$archive"
