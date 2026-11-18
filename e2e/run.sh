#!/bin/sh
# Runs the end-to-end suite: ./run.sh [playwright args], e.g. ./run.sh tests/04
# Needs Go and Node; uses the Chromium Playwright installs (npx playwright install chromium).
set -u
cd "$(dirname "$0")"
[ -d node_modules ] || npm ci --no-audit --no-fund
npx playwright test "$@" > playwright-report.log 2>&1
code=$?
node summary.mjs
echo "full log: e2e/playwright-report.log · report: npx playwright show-report"
exit $code
