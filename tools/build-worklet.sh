#!/usr/bin/env bash
# tools/build-worklet.sh — 제품 워클릿 엔진(cmd/worklet) TinyGo 빌드 → app/web/engine.wasm
# 어디서 호출해도 레포 루트 기준. 산출 크기(raw/gzip)를 stdout에 출력. import 0이어야 한다(wasm-unknown).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
mkdir -p app/web
tinygo build -target=wasm-unknown -gc=leaking -no-debug -opt=2 -o app/web/engine.wasm ./cmd/worklet
if command -v wasm-opt >/dev/null 2>&1; then
  if wasm-opt -O3 --enable-bulk-memory --enable-nontrapping-float-to-int -o app/web/engine.opt.wasm app/web/engine.wasm 2>/dev/null; then
    mv app/web/engine.opt.wasm app/web/engine.wasm
  fi
fi
RAW=$(wc -c < app/web/engine.wasm | tr -d ' ')
GZ=$(gzip -c app/web/engine.wasm | wc -c | tr -d ' ')
echo "app/web/engine.wasm ${RAW} bytes, gzip ${GZ} bytes"
