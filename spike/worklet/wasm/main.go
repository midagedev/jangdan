// spike/worklet/wasm — AudioWorklet 안에서 돌 순수 wasm 래퍼.
// import는 engine뿐. wasm-unknown 타깃: JS 글로벌 참조 0, JS 쪽에서
// WebAssembly.Instance 후 _initialize() 1회 호출이 필요(전역 초기화).
package main

import "github.com/midagedev/jangdan/engine"

var eng *engine.Engine
var out [2 * engine.Block]float32

// jd_init — 엔진 생성. 재호출은 새 엔진으로 교체하는데 -gc=leaking이라 이전
// 엔진 메모리는 회수되지 않는다(선형 증가). 스파이크 전제: 페이지당 1회.
//
//export jd_init
func jd_init(seed uint32) { eng = engine.New(seed) }

// jd_render — eng.Render를 max(1,mult)번 호출(부하 승수). 마지막 결과가 out에 남는다.
//
//export jd_render
func jd_render(mult uint32) {
	if eng == nil {
		return
	}
	if mult < 1 {
		mult = 1
	}
	for i := uint32(0); i < mult; i++ {
		eng.Render(out[:])
	}
}

// jd_out_ptr — out 배열 시작 주소(JS에서 Float32Array 뷰로 감쌈).
//
//export jd_out_ptr
func jd_out_ptr() *float32 { return &out[0] }

//
//export jd_set_param
func jd_set_param(id uint32, v float32) {
	if eng == nil {
		return
	}
	eng.SetParam(engine.ParamID(id), v)
}

//
//export jd_param
func jd_param(id uint32) float32 {
	if eng == nil {
		return 0
	}
	return eng.Param(engine.ParamID(id))
}

func main() {}
