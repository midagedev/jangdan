// Package room — 방 뷰(스텁; T7 라운드가 교체). 계약: New(ctx) core.View + DeviceTapped().
package room

import (
	"github.com/hajimehoshi/ebiten/v2"

	"github.com/midagedev/revirth/app/core"
)

type View struct{}

func New(ctx *core.Ctx) (*View, error) { return &View{}, nil }

func (v *View) Update(ctx *core.Ctx)                     {}
func (v *View) Draw(screen *ebiten.Image, ctx *core.Ctx) {}

// DeviceTapped — 이 프레임에 기기 영역이 탭됐는가(main.go가 기기 뷰로 전환).
func (v *View) DeviceTapped() bool { return false }
