// buttons.go — 버튼·패드·스텝·이름판 입력 → Cmd. 버튼류는 누르는 순간 동작하고,
// 패드는 탭(짧게)과 길게 누르기(뮤트)를 구분하므로 놓을 때 판정한다.
package device

import (
	"strconv"
	"strings"

	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

type btnKind uint8

const (
	bkWaveSaw btnKind = iota
	bkWaveSqr
	bkModeSlide
	bkModeAcc
	bkOctDown
	bkOctUp
	bkPat  // arg = 슬롯 0..3
	bkStep // arg = 스텝 0..15
	bkPlay
	bkRec
)

// editMode — 스텝 편집 모드. 둘 중 하나 또는 없음(전역 단일).
type editMode uint8

const (
	emNone editMode = iota
	emSlide
	emAcc
)

type button struct {
	name  string
	label string
	kind  btnKind
	arg   int
	sec   uint8
	rect  core.Rect
}

type padCtl struct {
	name     string
	part     engine.Part
	rect     core.Rect
	litUntil float64 // 탭 직후 120ms lit
	litA     float32 // 이번 프레임 lit 합성 알파(drawPadLit이 남긴다 — 테스트·디버그 관측용)
}

func rectHit(r core.Rect, x, y float64) bool {
	return x >= r[0]-hitRectPad && y >= r[1]-hitRectPad &&
		x < r[0]+r[2]+hitRectPad && y < r[1]+r[3]+hitRectPad
}

func (v *View) hitButton(x, y float64) int {
	for i := range v.buttons {
		if rectHit(v.buttons[i].rect, x, y) {
			return i
		}
	}
	return -1
}

func (v *View) hitPad(x, y float64) int {
	for i := range v.pads {
		if rectHit(v.pads[i].rect, x, y) {
			return i
		}
	}
	return -1
}

// buttonLabel — 버튼 위 폰트 라벨(대문자화 매핑).
func buttonLabel(name string, kind btnKind, arg int) string {
	switch kind {
	case bkPat:
		return string(rune('A' + arg))
	case bkStep:
		return strconv.Itoa(arg + 1)
	case bkPlay:
		return "PLAY"
	case bkRec:
		return "DROP"
	}
	switch name {
	case "saw":
		return "SAW"
	case "sqr":
		return "SQR"
	case "slide":
		return "SLIDE"
	case "acc":
		return "ACC"
	case "oct-":
		return "OCT-"
	case "oct+":
		return "OCT+"
	}
	return strings.ToUpper(name)
}

// bassParam — 베이스라인 섹션 → 파라미터 베이스.
func bassParam(sec uint8) engine.ParamID {
	if sec == secBassB {
		return engine.BassBParams
	}
	return engine.BassAParams
}

// transportLit — PLAY 버튼 lit 판정: 재생 중이거나 제스처 전(가짜 시계로 시각적으로
// 도는 중 — §12.3). 순수 함수(단언 대상).
func transportLit(t core.Tick) bool { return t.Playing || !t.Started }

// pressButton — 버튼 누름. saw/sqr→BWave, slide/acc→편집 모드 토글,
// oct→BOct 3단, pat→SelectPattern, step→선택 파트 편집, play→Transport(PLAY/STOP 토글),
// rec→Drop(+DropTapped 1프레임).
func (v *View) pressButton(ctx *core.Ctx, i int) {
	b := &v.buttons[i]
	switch b.kind {
	case bkWaveSaw, bkWaveSqr:
		val := float32(0)
		if b.kind == bkWaveSqr {
			val = 1
		}
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.SetParam, A: uint8(bassParam(b.sec) + engine.BWave), V: val}, core.Human)
	case bkModeSlide:
		if v.mode == emSlide {
			v.mode = emNone
		} else {
			v.mode = emSlide
		}
	case bkModeAcc:
		if v.mode == emAcc {
			v.mode = emNone
		} else {
			v.mode = emAcc
		}
	case bkOctDown, bkOctUp:
		cur := ctx.Bridge.Param(bassParam(b.sec) + engine.BOct)
		st := 0
		if cur < 1.0/3 {
			st = -1
		} else if cur >= 2.0/3 {
			st = 1
		}
		if b.kind == bkOctDown {
			if st > -1 {
				st--
			}
		} else if st < 1 {
			st++
		}
		val := float32(0.5)
		if st < 0 {
			val = 0.15
		} else if st > 0 {
			val = 0.85
		}
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.SetParam, A: uint8(bassParam(b.sec) + engine.BOct), V: val}, core.Human)
	case bkPat:
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.SelectPattern, A: uint8(b.sec), B: uint8(b.arg)}, core.Human)
	case bkStep:
		v.tapStep(ctx, b.arg)
	case bkPlay:
		a := uint8(1) // 정지 중 → 재생
		if ctx.Tick.Playing {
			a = 0 // 재생 중 → 정지
		}
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.Transport, A: a}, core.Human)
	case bkRec:
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.Drop}, core.Human)
		v.drop = true
	}
}

// tapStep — 16스텝 버튼. 선택 파트(기본 BassA, 패드/베이스라인 이름판 탭으로 변경)를 편집한다.
// 베이스는 편집 모드(기본=게이트, slide/acc)에 따라 플래그 비트를 토글하고 note·나머지 플래그는 보존.
func (v *View) tapStep(ctx *core.Ctx, step int) {
	p := v.selPart
	if p <= engine.BassB {
		note, flags := ctx.Bridge.BassStep(p, step)
		switch v.mode {
		case emSlide:
			flags ^= engine.StepSlide
		case emAcc:
			flags ^= engine.StepAccent
		default:
			flags ^= engine.StepGate
		}
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.BassStep, A: uint8(p), B: uint8(step), C: note, D: flags}, core.Human)
		return
	}
	flags := ctx.Bridge.DrumStep(p, step)
	if v.mode == emAcc {
		flags ^= engine.StepAccent
	} else {
		flags ^= engine.StepGate
	}
	ctx.Bridge.Cmd(engine.Cmd{Kind: engine.DrumStep, A: uint8(p), B: uint8(step), D: flags}, core.Human)
}

// holdPad — 패드 길게 누르기(≥ padHoldMute): 뮤트 토글. 한 번의 누름에 1회만.
func (v *View) holdPad(ctx *core.Ctx, i int) {
	p := &v.pads[i]
	b := uint8(1)
	if ctx.Bridge.Muted(p.part) {
		b = 0
	}
	ctx.Bridge.Cmd(engine.Cmd{Kind: engine.Mute, A: uint8(p.part), B: b}, core.Human)
}

// tapPad — 패드 탭: 즉시 원샷 + 그 파트를 스텝 편집 대상으로 + padLitDur lit.
func (v *View) tapPad(ctx *core.Ctx, i int) {
	p := &v.pads[i]
	ctx.Bridge.Cmd(engine.Cmd{Kind: engine.Trigger, A: uint8(p.part)}, core.Human)
	v.selPart = p.part
	p.litUntil = ctx.Now + padLitDur
}

// stepGate — 스텝 LED(중간 밝기) 판정용: 선택 파트의 스텝 게이트.
func (v *View) stepGate(ctx *core.Ctx, step int) bool {
	p := v.selPart
	if p <= engine.BassB {
		_, flags := ctx.Bridge.BassStep(p, step)
		return flags&engine.StepGate != 0
	}
	return ctx.Bridge.DrumStep(p, step)&engine.StepGate != 0
}
