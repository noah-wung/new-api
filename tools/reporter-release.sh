#!/bin/sh
set -eu

REPO_ROOT="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
cd "$REPO_ROOT"
SIGNING_KEY_ID="${REPORTER_RELEASE_KEY_ID:-release-1}"

if [ "$(uname -s)" = Darwin ] && [ "${REPORTER_RELEASE_KEY_PASSPHRASE+x}" != x ]; then
  exec go run ./cmd/reporter-release \
    --encrypted-key-file "release/signing/$SIGNING_KEY_ID.key.age" \
    --public-keys-file release/signing/public-keys.json \
    --key-id "$SIGNING_KEY_ID" \
    --keychain-service new-api.reporter-release "$@"
fi

exec go run ./cmd/reporter-release \
  --encrypted-key-file "release/signing/$SIGNING_KEY_ID.key.age" \
  --public-keys-file release/signing/public-keys.json \
  --key-id "$SIGNING_KEY_ID" "$@"
