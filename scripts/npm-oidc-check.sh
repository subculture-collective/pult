#!/usr/bin/env bash
# Checks that npm trusts this GitHub Actions workflow to publish each PACKAGE,
# without publishing anything. For each package it asks npm to exchange the
# job's OIDC token for a publish token, the same request `npm publish` makes,
# and discards the token. Needs id-token: write.
# Usage: scripts/npm-oidc-check.sh PACKAGE...
set -euo pipefail
: "${ACTIONS_ID_TOKEN_REQUEST_URL:?run this in GitHub Actions with id-token: write}"

id_token=$(curl -fsS -H "Authorization: Bearer ${ACTIONS_ID_TOKEN_REQUEST_TOKEN}" \
  "${ACTIONS_ID_TOKEN_REQUEST_URL}&audience=npm:registry.npmjs.org" |
  node -e 'process.stdout.write(JSON.parse(require("fs").readFileSync(0)).value)')
echo "::add-mask::${id_token}"

failed=0
for pkg in "$@"; do
  escaped=${pkg/\//%2f}
  # The response body holds a short-lived publish token: write it nowhere.
  code=$(curl -sS -o /dev/null -w '%{http_code}' -X POST \
    -H "Authorization: Bearer ${id_token}" \
    "https://registry.npmjs.org/-/npm/v1/oidc/token/exchange/package/${escaped}")
  if [[ $code == 2* ]]; then
    echo "$pkg: npm trusts this workflow"
  else
    echo "::error::$pkg: npm refused this workflow (HTTP $code); check the package's trusted publisher"
    failed=1
  fi
done
exit "$failed"
