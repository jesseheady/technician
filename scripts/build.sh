#!/usr/bin/env bash

set -euo pipefail

cd "$(dirname "$0")/.."

# CGO_ENABLED=0 pins a static, host-toolchain-independent build so the output is
# reproducible regardless of whether the build host has a C compiler. This project
# is pure Go, so cgo is never wanted. release.yml also sets it; keeping it here
# means a local `GOOS=… GOARCH=… ./scripts/build.sh` reproduces the release sha.
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o technician .

# The tarball is the unit people redistribute, so a statically linked
# binary has to carry its dependencies' attribution inside the archive,
# not merely alongside it as a sibling release asset. Regenerate every run:
# the notice is per-GOOS, so a stale file from a different target must never
# be reused (see #303).
./scripts/gen-licenses.sh "THIRD_PARTY_LICENSES-${GOOS}.txt"

FILES=(technician LICENSE "THIRD_PARTY_LICENSES-${GOOS}.txt")

# Use the commit date of HEAD for reproducibility.
SOURCE_DATE_EPOCH="$(git show -s --format=%ct)"

# if this is Linux we should have GNU tar
if [[ "$(uname -s)" == "Linux" ]]; then
  /usr/bin/tar -c \
    --owner=0 --group=0 \
    --numeric-owner \
    --mtime="@$SOURCE_DATE_EPOCH" \
    --sort=name \
    "${FILES[@]}" \
    | gzip -cn9 \
    > "technician-${GOOS}-${GOARCH}.tar.gz"

elif [[ "$(uname -s)" == "Darwin" ]]; then
  # macOS libarchive doesn't support --mtime
  # so we have to touch the files first
  find "${FILES[@]}" -exec env TZ=UTC \
    touch -t "$(date -ur "$SOURCE_DATE_EPOCH" +%Y%m%d%H%M.%S)" {} +
  /usr/bin/tar -c \
    --owner=0 --group=0 \
    --numeric-owner \
    --no-mac-metadata \
    --no-xattr \
    "${FILES[@]}" \
    | gzip -cn9 \
    > "technician-${GOOS}-${GOARCH}.tar.gz"

else
  echo "build.sh: unsupported host $(uname -s); need Linux (GNU tar) or Darwin" >&2
  exit 1
fi
