// Package assets — UI 스프라이트를 go:embed로 싣는다.
// 파일명·픽셀 크기·피벗은 자산 계약(CLAUDE.md "UI 스프라이트"):
// knob 256, pad/pad-lit 192, step/step-lit 96, panel 1080×1920, scope-frame 512×192.
// 플레이스홀더는 fal.ai 생성물로 같은 파일명으로 교체된다 — 코드는 크기를
// 하드코딩하지 않고 디코딩한 이미지에서 읽는다.
package assets

import (
	_ "embed"
	_ "image/png" // PNG 디코더 등록
)

var (
	//go:embed panel.png
	PanelPNG []byte
	//go:embed knob.png
	KnobPNG []byte
	//go:embed pad.png
	PadPNG []byte
	//go:embed pad-lit.png
	PadLitPNG []byte
	//go:embed step.png
	StepPNG []byte
	//go:embed step-lit.png
	StepLitPNG []byte
	//go:embed scope-frame.png
	ScopeFramePNG []byte
)
