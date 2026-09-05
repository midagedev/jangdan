#!/usr/bin/env bash
# tools/reports-pull.sh — Worker(KV)에 쌓인 계측 리포트를 내려받는다.
#   bash tools/reports-pull.sh [outdir=spike/worklet/results/remote]
# 필요: ~/.zshrc 의 JANGDAN_REPORTS_URL(Worker 주소)와 JANGDAN_ADMIN_TOKEN(wrangler secret put ADMIN_TOKEN 과 같은 값).
set -euo pipefail
OUT=${1:-spike/worklet/results/remote}; mkdir -p "$OUT"
URL=${JANGDAN_REPORTS_URL:?set JANGDAN_REPORTS_URL in ~/.zshrc}; TOKEN=${JANGDAN_ADMIN_TOKEN:?set JANGDAN_ADMIN_TOKEN in ~/.zshrc}
LIST=$(curl -sS "$URL/reports?token=$TOKEN")
echo "$LIST" | python3 -c 'import json,sys; d=json.load(sys.stdin); [print(k["id"], k.get("kind"), k.get("platform"), "load", k.get("load"), "stalls", k.get("stalls")) for k in d["keys"]]'
for id in $(echo "$LIST" | python3 -c 'import json,sys; [print(k["id"]) for k in json.load(sys.stdin)["keys"]]'); do
  [[ -f "$OUT/$id.json" ]] || curl -sS "$URL/reports/$id?token=$TOKEN" -o "$OUT/$id.json"
done
echo "saved to $OUT ($(ls "$OUT" | wc -l | tr -d ' ') files)"
