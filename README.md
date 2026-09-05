# 장단 · Jangdan

다락방에서 밤새 도는, 브라우저 액시드 신스 + 노동요 오토파일럿. 만지면 악기, 두면 라디오, 모이면 방송.

**상태: Phase 0(2026-09).** 판정 스파이크 둘이 통과했습니다.

- 엔진 `engine/` — 순수 Go, TinyGo `wasm-unknown`으로 빌드해 AudioWorklet 안에서 돕니다. 네이티브 렌더와 브라우저 오프라인 렌더가 샘플 단위로 같습니다(SHA-256 일치, 300초).
- UI `app/` — Ebitengine(Go wasm). wasm gzip 2.5MB, 세로 540×960 @2×에서 프레임 P95 1.5ms.
- 기기 뷰 아트 — `tools/rack/` 와이어프레임 → 채색 → 절단 파이프라인.

라이브 스파이크: https://midagedev.github.io/jangdan/

## 빌드

```bash
go test ./engine/ && bash tools/check-fma.sh      # 엔진 게이트(무할당·결정론·FMA 0)
bash spike/worklet/build.sh                        # TinyGo 워클릿 엔진
bash app/build.sh                                  # Ebitengine UI (GOOS=js GOARCH=wasm)
node spike/worklet/serve.mjs                       # https://localhost:8443 계측 페이지
```

요구: Go 1.26, TinyGo 0.42, binaryen(선택), Node 24. 규칙과 실측 노트는 `CLAUDE.md`, 문서는 `docs/`.

## 라이선스

코드는 MIT(`LICENSE`). 이름·캐릭터·아트 자산은 별도 고지(`ASSETS-LICENSE`).
