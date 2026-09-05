// Package device — 기기 뷰(스텁; T5 라운드가 교체). 계약: New(ctx) core.View.
package device

import (
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/midagedev/revirth/app/core"
	"github.com/midagedev/revirth/engine"
)

type View struct{}

func New(ctx *core.Ctx) (*View, error) { return &View{}, nil }

func (v *View) Update(ctx *core.Ctx)                     {}
func (v *View) Draw(screen *ebiten.Image, ctx *core.Ctx) {}

// BackTapped — 이 프레임에 이름판(타이틀 plate)이 탭됐는가(main.go가 방 뷰로 복귀).
func (v *View) BackTapped() bool { return false }

// JustGrabbed — 이 프레임에 사용자가 새로 잡은 노브(MANUAL 잠금용). 스텁: 없음.
func (v *View) JustGrabbed() (engine.ParamID, bool) { return 0, false }

// ResumeTapped / DropTapped — RESUME·DROP 버튼 탭(한 프레임만 true). 스텁: 없음.
func (v *View) ResumeTapped() bool { return false }
func (v *View) DropTapped() bool   { return false }
