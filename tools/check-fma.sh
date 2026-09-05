#!/usr/bin/env bash
# check-fma.sh — engine/ 네이티브 빌드에 FMA 명령이 없음을 단언한다.
# 왜: Go는 arm64에서 x*y+z를 FMADD로 융합하고 wasm은 융합하지 않아, 융합이 하나라도
# 남으면 네이티브 300초 렌더 해시와 브라우저 오프라인 렌더 해시가 어긋난다.
# 무엇을 막나: 곱을 잘못 감싼 형태(float32(x)*y+z, float32(x*y+z), 맨 x*y+z)가
# 리뷰를 통과해 들어오는 것. 곱만 정확히 감싼 float32(x*y)+z 와 mul32(x,y)+z 는 통과한다
# (2026-09-05 objdump 실측, go1.26.4 darwin/arm64).
# 사용: bash tools/check-fma.sh   (exit 0 = FMA 0개)
set -euo pipefail
cd "$(dirname "$0")/.."
tmp=$(mktemp -d); trap 'rm -rf "$tmp"' EXIT
GOARCH=arm64 GOOS=darwin go build -o "$tmp/engine.a" ./engine
hits=$(go tool objdump "$tmp/engine.a" | grep -E '\bF(N)?M(ADD|SUB)[SD]\b' || true)
if [[ -n "$hits" ]]; then
  echo "check-fma: FMA fusion found in engine/ (native arm64 build):" >&2
  echo "$hits" >&2
  echo "$(echo "$hits" | wc -l | tr -d ' ') hit(s) — wrap the product exactly: float32(x*y)+z or mul32(x,y)+z" >&2
  exit 1
fi
echo "check-fma: ok — 0 FMA instructions in engine/ (arm64)"
