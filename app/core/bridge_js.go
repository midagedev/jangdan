//go:build js && wasm

// bridge_js.go — window.jd(app/web/host.js)와의 접점. syscall/js는 이 파일에만.
// 호스트 API 계약(호스트 구현 라운드가 이 표를 그대로 구현한다 — docs/impl-plan-2026-09-05.md §4):
//
//	jd.start()                                   오디오 시작(제스처 뒤; 중복 무해)
//	jd.cmd(kind,a,b,c,d,v,author)                명령 예약(at = 현재 블록+2) + 로그 기록
//	jd.tick() → {started,block,step,bar,flags,peak,ctxTime}   최신 스냅샷(flags는 호출 사이 누적, 읽으면 0으로)
//	jd.scope() → Uint8Array(1024) | null         파형 256 Float32 뷰
//	jd.param(id) → number                        호스트 미러 값(0..1)
//	jd.devParam(slot,k) → number                 장치 로컬 파라미터 미러 값(0..1; 섀도 부재 −1)
//	jd.bassStep(part,step) → note | flags<<8     jd.drumStep(part,step) → flags   jd.muted(part) → 0|1   jd.slot(part) → n
//	jd.keyRoot() → 0..11   jd.chord(bar) → deg | flags<<8   jd.mode(part) → mode | dir<<8   jd.hint(state)   (Phase 2 §12)
//	tick에 playing(bool) 추가
//	jd.telemetry(event, value)                   jd.replay(seconds)   jd.seedWord() → string
//	jd.reducedMotion() → bool   jd.hidden() → bool   jd.wallClock() → [h,m,s]
//	jd.frame(ms)  jd.firstFrame()  jd.allocPerFrame(bytes)   계측
package core

import (
	"syscall/js"

	"github.com/midagedev/jangdan/engine"
)

type jsBridge struct {
	b js.Value
	// 프레임당 할당을 줄이기 위한 캐시
	tickKeys [7]js.Value
}

func NewBridge() Bridge {
	b := js.Global().Get("jd")
	if !b.Truthy() {
		return nopBridge{}
	}
	return &jsBridge{b: b}
}

func (j *jsBridge) Start() { j.b.Call("start") }

func (j *jsBridge) Cmd(c engine.Cmd, a Author) {
	j.b.Call("cmd", int(c.Kind), int(c.A), int(c.B), int(c.C), int(c.D), float64(c.V), int(a))
}

func (j *jsBridge) Tick() Tick {
	t := j.b.Call("tick")
	if !t.Truthy() {
		return Tick{}
	}
	tk := Tick{
		Started: t.Get("started").Truthy(),
		Block:   floatOf(t.Get("block")),
		Step:    intOf(t.Get("step")),
		Bar:     uint32(intOf(t.Get("bar"))),
		Flags:   uint32(intOf(t.Get("flags"))),
		Peak:    float32(floatOf(t.Get("peak"))),
		CtxTime: floatOf(t.Get("ctxTime")),
		Playing: t.Get("playing").Truthy(),
	}
	// levels — Float32Array(8) 또는 배열. 없거나 짧으면 0(구 호스트 호환). 프레임당 Get 8회.
	if lv := t.Get("levels"); lv.Truthy() {
		n := lv.Length()
		if n > NumLevels {
			n = NumLevels
		}
		for i := 0; i < n; i++ {
			tk.Levels[i] = float32(floatOf(lv.Index(i)))
		}
	}
	return tk
}

// RackRev / RackKind / Cables — 랙 위상(§14.3). Cables는 케이블당 JS 호출 2회라
// 비싸다 — 뷰가 RackRev 변화에서만 부른다는 계약이 이 비용의 상한이다.
func (j *jsBridge) RackRev() uint32 { return uint32(intOf(j.b.Call("rackRev"))) }

func (j *jsBridge) RackKind(slot int) engine.DeviceKind {
	k := intOf(j.b.Call("rackKind", slot))
	if k < 0 || k >= int(engine.NumDeviceKinds) {
		return engine.KindNone
	}
	return engine.DeviceKind(k)
}

func (j *jsBridge) Cables(dst []RackCable) int {
	n := intOf(j.b.Call("numCables"))
	if n > len(dst) {
		n = len(dst)
	}
	out := 0
	for i := 0; i < n; i++ {
		w := intOf(j.b.Call("cable", i))
		if w < 0 { // 0xFFFFFFFF(없음) — intOf가 −1로 준다
			continue
		}
		dst[out] = RackCable{
			Src:  uint8(w & 0xF),
			SP:   uint8((w >> 4) & 0xF),
			Dst:  uint8((w >> 8) & 0xF),
			DP:   uint8((w >> 12) & 0xF),
			Bind: uint8((w >> 16) & 0x3F),
			Gain: float32(floatOf(j.b.Call("cableGain", i))),
		}
		out++
	}
	return out
}

func (j *jsBridge) KeyRoot() int { return intOf(j.b.Call("keyRoot")) }

func (j *jsBridge) Chord(bar int) (uint8, uint8) {
	v := intOf(j.b.Call("chord", bar))
	return uint8(v & 0xFF), uint8(v >> 8)
}

func (j *jsBridge) Mode(p engine.Part) (uint8, uint8) {
	v := intOf(j.b.Call("mode", int(p)))
	return uint8(v & 0xFF), uint8(v >> 8)
}

func (j *jsBridge) Hint(state int) { j.b.Call("hint", state) }

func (j *jsBridge) Scope(dst []byte) bool {
	arr := j.b.Call("scope")
	if !arr.Truthy() || arr.Length() != len(dst) {
		return false
	}
	return js.CopyBytesToGo(dst, arr) == len(dst)
}

func (j *jsBridge) Param(id engine.ParamID) float32 {
	return float32(floatOf(j.b.Call("param", int(id))))
}

func (j *jsBridge) DevParam(slot, k int) float32 {
	return float32(floatOf(j.b.Call("devParam", slot, k)))
}

func floatOf(v js.Value) float64 {
	if v.Type() == js.TypeNumber {
		return v.Float()
	}
	return 0
}

func (j *jsBridge) BassStep(p engine.Part, step int) (uint8, uint8) {
	v := intOf(j.b.Call("bassStep", int(p), step))
	return uint8(v & 0xFF), uint8(v >> 8)
}

func (j *jsBridge) DrumStep(p engine.Part, step int) uint8 {
	return uint8(intOf(j.b.Call("drumStep", int(p), step)))
}

func (j *jsBridge) Muted(p engine.Part) bool { return intOf(j.b.Call("muted", int(p))) != 0 }
func (j *jsBridge) Slot(p engine.Part) uint8 { return uint8(intOf(j.b.Call("slot", int(p)))) }

// intOf — 호스트가 number 대신 boolean/undefined를 돌려줘도 패닉하지 않는다(2026-09-05: jd.muted가
// boolean을 돌려줘 기기 뷰 첫 Draw에서 "call of Value.Int on boolean" 패닉으로 앱이 죽었다).
func intOf(v js.Value) int {
	switch v.Type() {
	case js.TypeNumber:
		return v.Int()
	case js.TypeBoolean:
		if v.Bool() {
			return 1
		}
	}
	return 0
}

func (j *jsBridge) Telemetry(ev string, v float64) { j.b.Call("telemetry", ev, v) }
func (j *jsBridge) Replay(sec float64)             { j.b.Call("replay", sec) }
func (j *jsBridge) SeedWord() string {
	v := j.b.Call("seedWord")
	if v.Type() != js.TypeString {
		return ""
	}
	return v.String()
}
func (j *jsBridge) ReducedMotion() bool { return j.b.Call("reducedMotion").Truthy() }
func (j *jsBridge) Hidden() bool        { return j.b.Call("hidden").Truthy() }

func (j *jsBridge) WallClock() (int, int, int) {
	a := j.b.Call("wallClock")
	if !a.Truthy() || a.Length() < 3 {
		return 0, 0, 0
	}
	return intOf(a.Index(0)), intOf(a.Index(1)), intOf(a.Index(2))
}

func (j *jsBridge) CleanScreen() bool { return j.b.Call("cleanScreen").Truthy() }

func (j *jsBridge) Frame(ms float64)        { j.b.Call("frame", ms) }
func (j *jsBridge) FirstFrame()             { j.b.Call("firstFrame") }
func (j *jsBridge) AllocPerFrame(b float64) { j.b.Call("allocPerFrame", b) }

// nopBridge — 호스트가 없을 때(js 빌드지만 window.jd 미정의) 대체.
type nopBridge struct{}

func (nopBridge) Start()                                   {}
func (nopBridge) Cmd(engine.Cmd, Author)                   {}
func (nopBridge) Tick() Tick                               { return Tick{} }
func (nopBridge) Scope([]byte) bool                        { return false }
func (nopBridge) Param(engine.ParamID) float32             { return 0 }
func (nopBridge) DevParam(int, int) float32                { return -1 }
func (nopBridge) BassStep(engine.Part, int) (uint8, uint8) { return 0, 0 }
func (nopBridge) DrumStep(engine.Part, int) uint8          { return 0 }
func (nopBridge) Muted(engine.Part) bool                   { return false }
func (nopBridge) Slot(engine.Part) uint8                   { return 0 }
func (nopBridge) KeyRoot() int                             { return 0 }
func (nopBridge) Chord(int) (uint8, uint8)                 { return 0, 0 }
func (nopBridge) Mode(engine.Part) (uint8, uint8)          { return 0, 0 }
func (nopBridge) Hint(int)                                 {}
func (nopBridge) Telemetry(string, float64)                {}
func (nopBridge) Replay(float64)                           {}
func (nopBridge) SeedWord() string                         { return "" }
func (nopBridge) ReducedMotion() bool                      { return false }
func (nopBridge) Hidden() bool                             { return false }
func (nopBridge) CleanScreen() bool                        { return false }
func (nopBridge) WallClock() (int, int, int)               { return 0, 0, 0 }
func (nopBridge) Frame(float64)                            {}
func (nopBridge) FirstFrame()                              {}
func (nopBridge) AllocPerFrame(float64)                    {}
