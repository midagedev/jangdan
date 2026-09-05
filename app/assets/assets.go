// Package assets — UI 자산. 작은 것(레이아웃 JSON·폰트 아틀라스)은 go:embed, 큰 PNG(패널·플레이트·스프라이트)는
// wasm 밖에서 읽는다: 브라우저는 host.js가 페이지 로드와 동시에 prefetch한 바이트를 window.jd.asset(name)로,
// 데스크톱은 embed FS로. 근거(2026-09-05 실측): PNG를 wasm에 embed하면 gzip이 +1.9MB(이미 압축된 바이트라
// 줄지 않음)로 예산 3.0MB를 깬다. 파일명이 계약이다 — Read의 name은 "device/panel.png" 꼴.
//
// device/  — tools/rack 산출(후보 v1): panel.png(720×1280, 256색 팔레트), layout.json(좌표의 단일 소유자),
//
//	sprites/knob-r{25,32,42}.png
//
// room/    — plate-*.png(720×1280 시간대 플레이트, 256색), layout.json(영역), 캐릭터·고양이 포즈 스프라이트
// font/    — tools/font/atlas.py 산출(Go Bold 22px, ASCII, BSD-3 — ASSETS-LICENSE)
package assets

import _ "embed"

//go:embed device/layout.json
var DeviceLayoutJSON []byte

//go:embed room/layout.json
var RoomLayoutJSON []byte

//go:embed font/atlas.png
var FontAtlasPNG []byte

//go:embed font/atlas.json
var FontAtlasJSON []byte

// Names — 브라우저 호스트가 prefetch해야 하는 파일 목록(app/web/host.js가 같은 목록을 가진다;
// build.sh가 app/web/assets/ 아래로 복사한다).
var Names = []string{
	"device/panel.png",
	"device/sprites/knob-r25.png",
	"device/sprites/knob-r32.png",
	"device/sprites/knob-r42.png",
	"room/plate-night.png",
}
