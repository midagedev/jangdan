# Project overlay — revirth (장단 / Jangdan)

레포 루트 `/Users/hckim/repo/revirth`. 공개 레포(https://github.com/midagedev/jangdan)이므로
코드 주석·문서·문자열에 코드네임 "revirth"를 **새로 쓰지 않는다**(모듈 경로
`github.com/midagedev/revirth`는 예외 — 이미 존재하는 import 경로). 금지어(주석·문자열
포함): Roland, TB-303, TR-808, TR-909, ReBirth, Winamp, Lofi Girl.

## 게이트 레시피 (리드가 재실행하는 것과 같은 명령)

- 엔진 유닛: `go test ./engine/ -count=1`
- 엔진 FMA: `bash /Users/hckim/repo/revirth/tools/check-fma.sh` (FMADD/FMSUB 0개, exit 0)
- 엔진 vet: `go vet ./engine/`
- TinyGo 워클릿 빌드: `bash /Users/hckim/repo/revirth/spike/worklet/build.sh` (wasm-unknown, import 0)
- 앱 빌드(브라우저 wasm): `bash /Users/hckim/repo/revirth/app/build.sh`; 데스크톱 타입체크 `go vet ./app/...`
- 전체: `go build ./... && go vet ./... && go test ./... -count=1`

## 엔진 함정 (engine/ 을 만지는 라운드는 전부)

- 허용 math: `Float32bits/Float32frombits/Abs/Sqrt`만. `math.Exp/Sin/Tanh/Pow/Log` 금지 — 근사식은
  `engine/approx.go`의 `exp2/sin5/mul32`를 쓰고, 새 근사가 필요하면 **자기 소유 파일**에 소문자 함수로 추가.
- 곱셈-덧셈은 `mul32(a,b)+z` 또는 `float32(a*b)+z`. `float32(a)*b+c`, `float32(a*b+c)`는 융합된다.
- 핫 루프 무할당: 슬라이스 append·클로저·인터페이스·맵 금지. `testing.AllocsPerRun`으로 단언.
- `fmt`·`os`·`panic` 경로 없음(wasm-unknown). 범위 밖 입력은 클램프.
- 결정론: 난수는 xorshift32만, 시간 참조 금지.

## 앱 함정 (app/ 을 만지는 라운드)

- Ebitengine v2.9: `ebiten.SetTPS(ebiten.SyncWithFPS)` 필수(120Hz에서 Update 스킵). 프레임당 힙 할당 ≤ 2KB —
  `DrawImageOptions`는 재사용, 프레임마다 `&ebiten.DrawImageOptions{}` 금지.
- `syscall/js`는 `*_js.go`(빌드 태그 `js && wasm`)에만. `go vet ./app/...`은 데스크톱 타깃으로 통과해야 한다.
- 좌표의 단일 소유자는 레이아웃 JSON(app/assets 아래 device 와 room 각각의 layout.json — 웨이브 2에서 생성).
  Go 코드에 픽셀 좌표 상수를 새로 두지 않는다.
