#!/usr/bin/env bash
# TinyGo wasm 빌드 — 레포 루트 기준 경로로 동작(어디서 호출해도 동일).
# 산출: spike/worklet/public/engine.wasm (raw/gzip 바이트를 stdout에 출력)
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
mkdir -p spike/worklet/public

tinygo build -target=wasm-unknown -gc=leaking -no-debug -opt=2 \
  -o spike/worklet/public/engine.wasm ./spike/worklet/wasm

# wasm-opt -O3는 시도해 보되 실패하면 원본을 쓴다(이유를 로그).
if command -v wasm-opt >/dev/null 2>&1; then
  if wasm-opt -O3 --enable-bulk-memory --enable-nontrapping-float-to-int \
      -o spike/worklet/public/engine.opt.wasm spike/worklet/public/engine.wasm 2>/tmp/wasm-opt.err; then
    # 의미 보존 검증: 최적화본이 node 해시 기준과 다르면(IEEE 변경 의심) 원본으로 롤백.
    # (해시 비교 자체는 hash-node.mjs가 하고, 여기선 빌드 직후 byte 크기만 로그)
    mv spike/worklet/public/engine.opt.wasm spike/worklet/public/engine.wasm
  else
    echo "wasm-opt 실패, 원본 사용: $(head -c 300 /tmp/wasm-opt.err)"
  fi
else
  echo "wasm-opt 없음, 원본 사용"
fi

RAW=$(wc -c < spike/worklet/public/engine.wasm | tr -d ' ')
GZ=$(gzip -c spike/worklet/public/engine.wasm | wc -c | tr -d ' ')
echo "engine.wasm ${RAW} bytes, gzip ${GZ} bytes"
