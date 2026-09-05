//go:build js && wasm

// jdBridge(app/web/bridge.js)와의 접점. syscall/js 의존은 이 파일로만 허용한다
// — 데스크톱 빌드(`go vet ./app/...`)가 이 파일을 타입체크하지 않는다.
package main

import "syscall/js"

type jsBridge struct {
	b     js.Value // window.jdBridge
	valid bool
}

func newBridge() Bridge {
	b := js.Global().Get("jdBridge")
	if !b.Truthy() {
		return jsBridge{}
	}
	return jsBridge{b: b, valid: true}
}

func (j jsBridge) SetParam(id int, v float32) {
	if j.valid {
		j.b.Call("setParam", id, float64(v))
	}
}

// Scope — AnalyserNode 파형 256 Float32를 바이트 뷰(Uint8Array)로 복사한다.
// js.CopyBytesToGo는 바이트 전용이라 브리지가 Uint8Array 뷰를 돌려준다(계약).
func (j jsBridge) Scope(dst []byte) bool {
	if !j.valid {
		return false
	}
	arr := j.b.Call("scope")
	if arr.IsNull() || arr.IsUndefined() || arr.Length() != len(dst) {
		return false
	}
	return js.CopyBytesToGo(dst, arr) == len(dst)
}

func (j jsBridge) Clock() float64 {
	if !j.valid {
		return 0
	}
	return j.b.Call("clock").Float()
}

func (j jsBridge) Frame(ms float64) {
	if j.valid {
		j.b.Call("frame", ms)
	}
}

func (j jsBridge) FirstFrame() {
	if j.valid {
		j.b.Call("firstFrame")
	}
}

func (j jsBridge) AllocPerFrame(bytes float64) {
	if j.valid {
		j.b.Call("allocPerFrame", bytes)
	}
}

func (j jsBridge) KnobDrag(id int, v float32) {
	if j.valid {
		j.b.Call("knobDrag", id, float64(v))
	}
}
