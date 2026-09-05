//go:build js && wasm

// share_js.go — 공유 URL 내보내기(DOM "share" 버튼 → window.jdShareURL()). syscall/js는 이 파일에 격리.
// 정본 로그는 호스트(JS)에 있지만 URL 인코딩(스텝 양자화·예산 감량)은 session 패키지 Go 구현이 소유하므로
// Go 미러 로그를 인코딩한다(둘은 같은 Cmd 스트림을 같은 순서로 받는다 — main.go send()).
package main

import (
	"syscall/js"

	"github.com/midagedev/revirth/session"
)

func installShare(g *game) {
	js.Global().Set("jdShareURL", js.FuncOf(func(this js.Value, args []js.Value) any {
		if g.in.log == nil || len(g.in.log.Entries) == 0 {
			return ""
		}
		u, _ := session.EncodeURLBudget(g.in.log, 2000)
		return u
	}))
}
