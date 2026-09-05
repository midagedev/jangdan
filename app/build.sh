#!/usr/bin/env bash
# app/build.sh — 레포 루트에서 실행: bash app/build.sh
# 표준 Go wasm(Ebitengine) 빌드 + GOROOT wasm_exec.js 복사 + 워클릿 산출물 복사
# (spike/worklet은 수정하지 않는다) + 사전압축 + 정지 첫 화면 + 크기 리포트.
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p app/web

GOOS=js GOARCH=wasm go build -ldflags='-s -w' -trimpath -o app/web/app.wasm ./app

GOROOT="$(go env GOROOT)"
cp "$GOROOT/lib/wasm/wasm_exec.js" app/web/wasm_exec.js

cp spike/worklet/public/engine.wasm spike/worklet/public/processor.js app/web/

gzip -9 -kf app/web/app.wasm
if command -v brotli >/dev/null 2>&1; then
  brotli -9 -kf app/web/app.wasm
else
  echo "brotli n/a"
fi

# 정지 첫 화면(still.png = panel.png의 540×960 축소본). 플레이스홀더가 없으면 먼저 생성.
[ -f app/assets/panel.png ] || go run ./app/tools/placeholders -out app/assets
go run ./app/tools/placeholders -out app/assets -still app/web/still.png

raw=$(wc -c < app/web/app.wasm | tr -d ' ')
gz=$(wc -c < app/web/app.wasm.gz | tr -d ' ')
if [ -f app/web/app.wasm.br ]; then br=$(wc -c < app/web/app.wasm.br | tr -d ' '); else br="n/a"; fi
echo "app.wasm $raw bytes, gzip $gz, brotli $br"
