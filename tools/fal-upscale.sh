#!/usr/bin/env bash
# fal-upscale.sh — 기존 이미지를 fal.ai 업스케일러로 확대(구도 보존). 승인된 컨셉컷을 해상도만 올릴 때 쓴다.
#   tools/fal-upscale.sh <in.png> <out.png> [model=fal-ai/esrgan] [scale=2]
# 입력은 data URI(base64)로 보낸다(수 MB 이하). 응답 원본을 <out>.json으로 남긴다. 키는 $FAL_KEY(~/.zshrc 단일 소유자).
set -euo pipefail
IN=$1; OUT=$2; MODEL=${3:-fal-ai/esrgan}; SCALE=${4:-2}
KEY=${FAL_KEY:-}
if [[ -z "$KEY" ]]; then KEY=$(grep -E '^export FAL_KEY=' ~/.zshrc | head -1 | sed -E 's/^export FAL_KEY="?([^"]*)"?/\1/'); fi
[[ -n "$KEY" ]] || { echo "FAL_KEY 없음" >&2; exit 2; }
MIME=$(file -b --mime-type "$IN")
BODY=$(python3 -c 'import json,base64,sys; print(json.dumps({"image_url":"data:%s;base64,%s"%(sys.argv[2],base64.b64encode(open(sys.argv[1],"rb").read()).decode()),"scale":int(sys.argv[3])}))' "$IN" "$MIME" "$SCALE")
RESP=$(curl -sS -X POST "https://fal.run/$MODEL" -w '\n%{http_code}' -H "Authorization: Key $KEY" -H "Content-Type: application/json" -d "$BODY")
CODE=${RESP##*$'\n'}; RESP=${RESP%$'\n'*}
if [[ "$CODE" != 2* ]]; then echo "fal-upscale: HTTP $CODE from $MODEL:" >&2; printf '%s\n' "$RESP" | head -c 600 >&2; exit 66; fi
printf '%s' "$RESP" > "${OUT%.*}.json"
URL=$(printf '%s' "$RESP" | python3 -c 'import json,sys; r=json.load(sys.stdin); print((r.get("images") or [r.get("image")])[0]["url"])')
curl -sS -L "$URL" -o "$OUT"
echo "saved $OUT ($(python3 -c 'import sys; from PIL import Image; print(Image.open(sys.argv[1]).size)' "$OUT")) <- $URL"
