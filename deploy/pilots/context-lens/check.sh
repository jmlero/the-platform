#!/bin/sh
set -eu
# The image pins manifests/dependencies. Candidate code cannot change this check.
(cd /opt/context-lens/baseline && sha256sum package.json package-lock.json) |
  while read -r digest file; do
    printf '%s  %s\n' "$digest" "$file" | sha256sum --check --status
  done
if [ ! -d node_modules ]; then
  cp -a /opt/context-lens/dependencies/node_modules ./node_modules
fi
case "${1:-baseline}" in
  baseline)
    npm test
    npm run check
    npm run lint
    npm run build
    ;;
  report)
    node_modules/.bin/tsx --test /opt/context-lens/trusted/report.test.mts
    ;;
  *) echo 'Use baseline or report' >&2; exit 2 ;;
esac
