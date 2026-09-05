//go:build js && wasm

package assets

import (
	"errors"
	"syscall/js"
	"time"
)

// Read — 큰 PNG 바이트. 브라우저: host.js가 prefetch해 둔 window.jd.asset(name)(Uint8Array)에서 복사.
func Read(name string) ([]byte, error) {
	jd := js.Global().Get("jd")
	if !jd.Truthy() {
		return nil, errors.New("assets: window.jd 없음")
	}
	arr := jd.Call("asset", name)
	if !arr.Truthy() {
		return nil, errors.New("assets: 미로드 " + name)
	}
	b := make([]byte, arr.Length())
	js.CopyBytesToGo(b, arr)
	return b, nil
}

// WaitReady — host.js의 prefetch가 끝날 때까지(최대 20초) 대기. 실패하면 false(뷰가 플레이스홀더로 진행).
func WaitReady() bool {
	jd := js.Global().Get("jd")
	if !jd.Truthy() {
		return false
	}
	for i := 0; i < 2000; i++ {
		r := jd.Call("assetsReady")
		if r.Truthy() {
			return r.Int() > 0
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}
