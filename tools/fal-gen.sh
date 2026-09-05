#!/usr/bin/env bash
# fal-gen.sh — generate one image via fal.ai (sync endpoint) and save it locally.
#
#   tools/fal-gen.sh <model> <out.png> '<prompt>' [extra-json-fields]
#   e.g. tools/fal-gen.sh fal-ai/recraft-v3 out.png 'a cozy room' '"style":"digital_illustration/pixel_art","image_size":"portrait_4_3"'
#
# Key: $FAL_KEY exported in ~/.zshrc (single owner). Never commit the key.
# Writes <out>.json (raw response) next to the image for provenance.
set -euo pipefail
MODEL=$1; OUT=$2; PROMPT=$3; EXTRA=${4:-}
KEY=${FAL_KEY:?FAL_KEY is not set — export it in ~/.zshrc (see CLAUDE.md)}
BODY=$(python3 -c 'import json,sys; d={"prompt":sys.argv[1]}; extra=sys.argv[2]; 
d.update(json.loads("{"+extra+"}") if extra else {}); print(json.dumps(d))' "$PROMPT" "$EXTRA")
# Recraft rejects prompts over 1000 chars with a bare 422 (measured 2026-09-05) — say so before the call.
if [[ "$MODEL" == fal-ai/recraft* && ${#PROMPT} -gt 1000 ]]; then echo "fal-gen: prompt is ${#PROMPT} chars; recraft-v3 caps at 1000 (422)" >&2; exit 65; fi
RESP=$(curl -sS -X POST "https://fal.run/$MODEL" -w '\n%{http_code}' \
  -H "Authorization: Key $KEY" -H "Content-Type: application/json" -d "$BODY")
CODE=${RESP##*$'\n'}; RESP=${RESP%$'\n'*}
if [[ "$CODE" != 2* ]]; then echo "fal-gen: HTTP $CODE from $MODEL — body:" >&2; printf '%s\n' "$RESP" | head -c 800 >&2; echo >&2; exit 66; fi
printf '%s' "$RESP" > "${OUT%.*}.json"
printf '%s\n' "$PROMPT" > "${OUT%.*}.prompt.txt"
URL=$(printf '%s' "$RESP" | python3 -c 'import json,sys; r=json.load(sys.stdin); print((r.get("images") or [r.get("image")])[0]["url"])')
curl -sS -L "$URL" -o "$OUT"
echo "saved $OUT ($(du -h "$OUT" | cut -f1)) <- $URL"
