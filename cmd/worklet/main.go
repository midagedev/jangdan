// cmd/worklet — AudioWorklet 안에서 돌 순수 wasm 래퍼(제품 진입점; spike/worklet은 계측용으로 동결).
// 빌드: bash tools/build-worklet.sh → app/web/engine.wasm (TinyGo -target wasm-unknown -gc=leaking).
// import는 engine뿐. JS 쪽은 WebAssembly.Instance 뒤 _initialize() 1회 호출 후 jd_init(seed).
// export 표의 원본: docs/impl-plan-2026-09-05.md §3. 이 파일에는 곱셈-덧셈이 없다.
package main

import "github.com/midagedev/revirth/engine"

var eng engine.Engine // 정적 할당 — New 대신 Reset으로 초기화(-gc=leaking에서 재할당 없음)
var out [2 * engine.Block]float32
var state [engine.StateSize]byte
var inited bool

//export jd_init
func jd_init(seed uint32) {
	eng.Reset(seed)
	inited = true
}

//export jd_reset
func jd_reset(seed uint32) { jd_init(seed) }

//export jd_render
func jd_render() {
	if !inited {
		return
	}
	eng.Render(out[:])
}

//export jd_out_ptr
func jd_out_ptr() *float32 { return &out[0] }

//export jd_cmd
func jd_cmd(kind, a, b, c, d uint32, v float32) {
	if !inited || kind >= uint32(engine.NumCmdKinds) {
		return
	}
	eng.Apply(engine.Cmd{Kind: engine.CmdKind(kind), A: uint8(a), B: uint8(b), C: uint8(c), D: uint8(d), V: v})
}

//export jd_param
func jd_param(id uint32) float32 { return eng.Param(engine.ParamID(id)) }

//export jd_flags
func jd_flags() uint32 { return eng.Flags() }

//export jd_step
func jd_step() uint32 { return uint32(eng.Step()) }

//export jd_bar
func jd_bar() uint32 { return eng.Bar() }

//export jd_block
func jd_block() float64 { return float64(eng.Block()) }

//export jd_peak
func jd_peak() float32 { return eng.Peak() }

//export jd_state_ptr
func jd_state_ptr() *byte { return &state[0] }

//export jd_state_size
func jd_state_size() uint32 { return uint32(engine.StateSize) }

//export jd_state_write
func jd_state_write() uint32 { return uint32(eng.WriteState(state[:])) }

//export jd_state_read
func jd_state_read(n uint32) uint32 {
	if n < uint32(engine.StateSize) || !eng.ReadState(state[:]) {
		return 0
	}
	return 1
}

// jd_bass_step / jd_drum_step — UI 표시용 패턴 읽기(호스트가 tick에 실어 보낸다).
//
//export jd_bass_step
func jd_bass_step(part, step uint32) uint32 {
	n, f := eng.BassStepAt(engine.Part(part), int(step))
	return uint32(n) | uint32(f)<<8
}

//export jd_drum_step
func jd_drum_step(part, step uint32) uint32 {
	return uint32(eng.DrumStepAt(engine.Part(part), int(step)))
}

//export jd_muted
func jd_muted(part uint32) uint32 {
	if eng.Muted(engine.Part(part)) {
		return 1
	}
	return 0
}

//export jd_slot
func jd_slot(part uint32) uint32 { return uint32(eng.Slot(engine.Part(part))) }

func main() {}
