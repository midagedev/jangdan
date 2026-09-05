#!/usr/bin/env bash
# fal-sprite.sh — UI 스프라이트 1장 생성: flux/dev 생성 → fal birefnet 배경 제거 → 알파 트림 →
# 정방형 패딩(피벗 = 중심) → PNG 저장. 원본·프롬프트·응답 JSON을 함께 남긴다(출처 추적).
#
#   tools/fal-sprite.sh <out.png> '<subject prompt>' [size=256] [extra-json-fields]
#   예) tools/fal-sprite.sh app/assets/knob.png 'A single chunky round rotary knob ...' 256
#   모델 교체: SPRITE_MODEL=fal-ai/recraft-v3 tools/fal-sprite.sh out.png '...' 256 '"style":"digital_illustration/hand_drawn","image_size":"square"'
#
# 스타일 계약(docs/concepts/README.md)을 프롬프트 앞머리에 자동으로 붙인다 — 새 룩을 발명하지 않는다.
# 실측(2026-09-05): birefnet은 1024 입력에 512×512 RGBA를 돌려준다. 배경은 "plain flat solid
# light grey background, nothing else, no ground shadow"로 지정해야 분리가 깨끗하다.
set -euo pipefail
OUT=$1; SUBJECT=$2; SIZE=${3:-256}; EXTRA=${4:-'"image_size":"square","guidance_scale":2.5,"num_inference_steps":28'}
KEY=${FAL_KEY:?FAL_KEY is not set — export it in ~/.zshrc (see CLAUDE.md)}
HERE=$(cd "$(dirname "$0")/.." && pwd)
STYLE=$(grep -m1 '^> Hand-drawn 1990s' "$HERE/docs/concepts/README.md" | sed 's/^> //')
[[ -n "$STYLE" ]] || { echo "fal-sprite: style contract line not found in docs/concepts/README.md" >&2; exit 65; }
BASE=${OUT%.*}
"$HERE/tools/fal-gen.sh" "${SPRITE_MODEL:-fal-ai/flux/dev}" "$BASE.raw.png" "$STYLE $SUBJECT isolated object centered on a plain flat solid light grey background, nothing else, no text, no ground shadow, sprite for a game UI" "$EXTRA" >&2
mv "$BASE.raw.json" "$BASE.gen.json"; mv "$BASE.raw.prompt.txt" "$BASE.prompt.txt"
URL=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["images"][0]["url"])' "$BASE.gen.json")
RESP=$(curl -sS -X POST https://fal.run/fal-ai/birefnet -w '\n%{http_code}' -H "Authorization: Key $KEY" -H 'Content-Type: application/json' -d "{\"image_url\":\"$URL\"}")
CODE=${RESP##*$'\n'}; RESP=${RESP%$'\n'*}
[[ "$CODE" == 2* ]] || { echo "fal-sprite: birefnet HTTP $CODE: ${RESP:0:400}" >&2; exit 66; }
printf '%s' "$RESP" > "$BASE.cut.json"
CUT=$(printf '%s' "$RESP" | python3 -c 'import json,sys;print(json.load(sys.stdin)["image"]["url"])')
curl -sS -L "$CUT" -o "$BASE.cut.png"
python3 - "$BASE.cut.png" "$OUT" "$SIZE" <<'PY'
import sys
from PIL import Image
src, out, size = sys.argv[1], sys.argv[2], int(sys.argv[3])
im = Image.open(src).convert("RGBA")
bbox = im.getchannel("A").getbbox()
if not bbox: sys.exit("fal-sprite: empty alpha — background removal found nothing")
im = im.crop(bbox)
w, h = im.size; side = max(w, h)
sq = Image.new("RGBA", (side, side), (0, 0, 0, 0)); sq.paste(im, ((side - w) // 2, (side - h) // 2))
sq = sq.resize((size, size), Image.LANCZOS)
sq.save(out, optimize=True)
print(f"fal-sprite: {out} {size}x{size} (trimmed {w}x{h} from {src})")
PY
