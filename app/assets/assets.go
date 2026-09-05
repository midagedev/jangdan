// Package assets — UI 자산을 go:embed로 싣는다. 파일명이 계약이다(코드는 크기를 디코딩한 이미지에서 읽는다).
//
// device/  — tools/rack 파이프라인 산출(후보 v1, 2026-09-05): panel.png(720×1280), layout.json(좌표의 단일 소유자),
//
//	sprites/knob-r{25,32,42}.png(반지름 클래스별 노브, 코드가 −135°..+135° 회전)
//
// room/    — plate-*.png(720×1280 시간대 플레이트), layout.json(영역), 캐릭터·고양이 포즈 스프라이트
package assets

import "embed"

//go:embed device/panel.png
var DevicePanelPNG []byte

//go:embed device/layout.json
var DeviceLayoutJSON []byte

//go:embed device/sprites/*.png
var DeviceSprites embed.FS

//go:embed room/layout.json
var RoomLayoutJSON []byte

//go:embed room/*.png
var RoomFiles embed.FS

// font/ — tools/font/atlas.py 산출(Go Bold 22px, ASCII, BSD-3 — ASSETS-LICENSE 참조)
//
//go:embed font/atlas.png
var FontAtlasPNG []byte

//go:embed font/atlas.json
var FontAtlasJSON []byte
