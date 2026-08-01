#!/usr/bin/env bash

set -euo pipefail

go build -trimpath -ldflags="-s -w" -o technician .

# The tarball is the unit people redistribute, so a statically linked
# binary has to carry its dependencies' attribution inside the archive,
# not merely alongside it as a sibling release asset.
# This is run once per GOOS to capture OS-specific dependencies.
[[ -f THIRD_PARTY_LICENSES-${GOOS}.txt ]] || \
  ./scripts/gen-licenses.sh THIRD_PARTY_LICENSES-${GOOS}.txt

FILES="technician LICENSE THIRD_PARTY_LICENSES-${GOOS}.txt"

# Use the commit date of HEAD for reproducibility.
SOURCE_DATE_EPOCH="$(git show -s --format=%ct)"

# if this is Linux we should have GNU tar
if [[ "$(uname -s)" == "Linux" ]]; then
  /usr/bin/tar -c \
    --owner=0 --group=0 \
    --numeric-owner \
    --mtime="@$SOURCE_DATE_EPOCH" \
    --sort=name \
    $FILES \
    | gzip -cn9 \
    > "technician-${GOOS}-${GOARCH}.tar.gz"

elif [[ "$(uname -s)" == "Darwin" ]]; then
  # macOS libarchive doesn't support --mtime
  # so we have to touch the files first
  find $FILES -exec env TZ=UTC \
    touch -t "$(date -ur "$SOURCE_DATE_EPOCH" +%Y%m%d%H%M.%S)" {} +
  /usr/bin/tar -c \
    --owner=0 --group=0 \
    --numeric-owner \
    --no-mac-metadata \
    --no-xattr \
    $FILES \
    | gzip -cn9 \
    > "technician-${GOOS}-${GOARCH}.tar.gz"
fi