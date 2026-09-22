#!/bin/sh
# Synthetic KINT-04 actor only; the model fixture requests this as exec_command.
set -eu
test ! -e /var/run/secrets/kubernetes.io/serviceaccount/token
echo kubernetes_token=absent
if [ -n "${OPENAI_API_KEY:-}" ]; then test "$OPENAI_API_KEY" = kagent-credential-injected; fi
echo model_key=absent_or_inert
for name in OPENROUTER_API_KEY GH_TOKEN GITHUB_TOKEN GIT_TOKEN; do eval 'value=${'"$name"':-}'; test -z "$value"; done
echo upstream_and_git_credentials=absent
wget -q -O /tmp/authority -T 5 https://kint04-gateway.kint-engine.svc/engine/authority
echo injected_authority=allowed
for path in engine/memory engine/delegate engine/git/git-receive-pack engine/tools/exec; do
 if wget -q -O /dev/null -T 3 --post-data='{}' "https://kint04-gateway.kint-engine.svc/$path" 2>/dev/null; then echo "$path=UNEXPECTED"; exit 1; else echo "$path=denied"; fi
done
for endpoint in https://api.openai.com/v1/models https://github.com https://kubernetes.default.svc https://kint-engine-controller.kint-engine.svc:8083; do
 if wget -q -O /dev/null -T 3 "$endpoint" 2>/dev/null; then echo direct_bypass=UNEXPECTED; exit 1; else echo "$endpoint=denied"; fi
done
echo probe_complete
