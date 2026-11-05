#!/usr/bin/env bash
set -euo pipefail
cd -- "$(dirname -- "$0")"
sha256sum --check --status SHA256SUMS
if python3 -c 'import json,sys;sys.exit(json.load(open("release.json")).get("authorization_model") != "account_spaces_v1")'; then
  exec python3 deploy-user-spaces.py
fi
exec python3 deploy.py
