#!/usr/bin/env bash
# check-wasm.sh — wasm 타깃 타입체크 게이트.
#
# 왜 따로 있는가(2026-09-06): `go vet ./app/...`는 데스크톱 태그만 본다. js 전용 파일
# (bridge_js.go)의 결함은 브라우저 빌드 시점까지 미뤄져 "테스트 전부 green인데 배포가 깨지는"
# 형태로 나타난다. 라운드 검수에서 이 한 줄을 같이 돌린다.
set -euo pipefail
cd "$(dirname "$0")/.."
GOOS=js GOARCH=wasm go build -o /dev/null ./app/ ./app/core/ ./app/view/... 
GOOS=js GOARCH=wasm go vet ./app/... 
echo "check-wasm: ok — js/wasm 빌드·vet 통과"
