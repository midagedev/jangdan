#!/usr/bin/env bash
# app/build.sh — 레포 루트에서 실행: bash app/build.sh
# 표준 Go wasm(Ebitengine) 빌드 + GOROOT wasm_exec.js 복사 + 제품 워클릿(cmd/worklet) 빌드 + 사전압축 + 크기 리포트.
# app/web/{index.html,host.js,processor.js}는 소스(호스트 라운드 소유) — 여기서 생성하지 않는다.
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p app/web
GOOS=js GOARCH=wasm go build -ldflags='-s -w' -trimpath -o app/web/app.wasm ./app
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" app/web/wasm_exec.js
bash tools/build-worklet.sh
# 큰 PNG는 wasm 밖(app/assets/assets.go Names) — 호스트가 prefetch하는 정적 경로로 복사
rm -rf app/web/assets && mkdir -p app/web/assets/device/sprites app/web/assets/room
cp app/assets/device/panel.png app/web/assets/device/ && cp app/assets/device/sprites/*.png app/web/assets/device/sprites/ && cp app/assets/room/*.png app/web/assets/room/
gzip -9 -kf app/web/app.wasm
if command -v brotli >/dev/null 2>&1; then brotli -9 -kf app/web/app.wasm; fi
raw=$(wc -c < app/web/app.wasm | tr -d ' '); gz=$(wc -c < app/web/app.wasm.gz | tr -d ' ')
br=$([ -f app/web/app.wasm.br ] && wc -c < app/web/app.wasm.br | tr -d ' ' || echo n/a)
echo "app.wasm $raw bytes, gzip $gz, brotli $br"
