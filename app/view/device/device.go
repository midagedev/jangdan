// Package device — 기기 뷰(스텁; T5 라운드가 교체). 계약: New(ctx) core.View.
package device

import (
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/midagedev/revirth/app/core"
)

type View struct{}

func New(ctx *core.Ctx) (*View, error) { return &View{}, nil }

func (v *View) Update(ctx *core.Ctx)                     {}
func (v *View) Draw(screen *ebiten.Image, ctx *core.Ctx) {}

// BackTapped — 이 프레임에 이름판(타이틀 plate)이 탭됐는가(main.go가 방 뷰로 복귀).
func (v *View) BackTapped() bool { return false }
