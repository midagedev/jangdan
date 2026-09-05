//go:build js && wasm

// share_js.go — 공유 URL 내보내기(DOM "share" 버튼 → window.jdShareURL()). syscall/js는 이 파일에 격리.
// 정본 로그는 호스트(JS)에 있지만 URL 인코딩(스텝 양자화·예산 감량)은 session 패키지 Go 구현이 소유하므로
// Go 미러 로그를 인코딩한다(둘은 같은 Cmd 스트림을 같은 순서로 받는다 — main.go send()).
package main

import (
	"syscall/js"

	"github.com/midagedev/revirth/session"
)

// sharedLog — URL의 s= 파라미터(session.EncodeURL 형식)를 디코드한다. 없거나 깨지면 nil(부분 결과는 살린다).
func sharedLog() *session.Log {
	loc := js.Global().Get("location")
	if !loc.Truthy() {
		return nil
	}
	q := js.Global().Get("URLSearchParams").New(loc.Get("search"))
	v := q.Call("get", "s")
	if v.Type() != js.TypeString || v.String() == "" {
		return nil
	}
	l, _ := session.DecodeURL(v.String())
	if l == nil || len(l.Entries) == 0 {
		return nil
	}
	return l
}

func installShare(g *game) {
	js.Global().Set("jdShareURL", js.FuncOf(func(this js.Value, args []js.Value) any {
		if g.in.log == nil || len(g.in.log.Entries) == 0 {
			return ""
		}
		u, _ := session.EncodeURLBudget(g.in.log, 2000)
		return u
	}))
}
