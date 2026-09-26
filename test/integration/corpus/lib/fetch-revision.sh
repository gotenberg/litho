#!/usr/bin/env bash
# fetch-revision.sh: check out one pinned revision of a git repository.
#
# The revision is a full commit SHA or a tag, never a branch: a branch moves,
# and a corpus or a tool built from it would change under the same pin. Only
# that one commit is fetched (depth 1), so a pin costs the same as the shallow
# clone it replaces.
#
# Usage: fetch-revision.sh <repository_url> <revision> <directory>
set -euo pipefail

URL="${1:?usage: fetch-revision.sh <repository_url> <revision> <directory>}"
REV="${2:?revision missing: corpus init passes it from test/integration/corpus/versions.env}"
DIR="${3:?directory missing}"

rm -rf "$DIR"
git init -q "$DIR"
git -C "$DIR" remote add origin "$URL"

REF="$REV"
if [[ ! "$REV" =~ ^[0-9a-f]{40}$ ]]; then
  REF="refs/tags/${REV}"
  if ! git -C "$DIR" ls-remote --exit-code origin "$REF" > /dev/null; then
    echo "ERROR: ${REV} is neither a full commit SHA nor a tag of ${URL}" >&2
    exit 1
  fi
fi

git -C "$DIR" fetch -q --depth 1 origin "$REF"
git -C "$DIR" -c advice.detachedHead=false checkout -q FETCH_HEAD
echo "  ${URL} at $(git -C "$DIR" rev-parse HEAD) (pinned ${REV})"
