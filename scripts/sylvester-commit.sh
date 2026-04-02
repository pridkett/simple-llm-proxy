#!/usr/bin/env bash
# sylvester-commit.sh — git commit as Sylvester Supreme with SSH signing
# Usage: ./scripts/sylvester-commit.sh [any git-commit args]
# Example: ./scripts/sylvester-commit.sh -m "feat(auth): add token refresh"

set -euo pipefail

TMPKEY=$(mktemp /tmp/sylvester_key.XXXXXX)
trap 'rm -f "$TMPKEY"' EXIT

op read "op://Private/xnvxfsm47vm6s24v5qtygdgopa/private key?ssh-format=openssh" > "$TMPKEY"
chmod 600 "$TMPKEY"

git -c user.name="Sylvester Supreme" \
    -c user.email="sylvester-supreme@w2research.com" \
    -c user.signingkey="$TMPKEY" \
    -c gpg.format=ssh \
    -c gpg.ssh.program=ssh-keygen \
    -c commit.gpgsign=true \
    commit "$@"
