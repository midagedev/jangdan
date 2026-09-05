//go:build js && wasm

// share_js.go — 공유 세션 내보내기·열기의 JS 접점. syscall/js는 이 파일에 격리한다.
// URL에는 세션 id만 싣는다(사용자 결정 2026-09-06 — docs/impl-plan-2026-09-05.md §12.6):
// jdShareURL()이 무손실 페이로드(session.EncodeURL — 감량 0단계, EncodeURLBudget은 공유
// 경로에서 더 쓰지 않는다)를 만들어 host.js jd.shareSession(payload, seed, word)에 넘기면
// JS가 키프레임 상태(state:get)를 붙여 Worker KV에 저장(POST /sessions)하고 id로 최종 URL을
// resolve한다(반환값이 Promise — 쿨다운 중에는 캐시 URL 문자열이 그대로 돌아온다).
// 열기(?s=)는 호스트가 페이지 로드 즉시 GET한 페이로드를 jd.sharedLog()로 돌려주고 여기서
// 디코드한다. 정본 로그는 호스트에 있지만 인코딩은 session Go 구현이 소유하므로 Go 미러
// 로그를 인코딩한다(둘은 같은 Cmd 스트림을 같은 순서로 받는다 — main.go cmdBridge).
package main

import (
	"syscall/js"

	"github.com/midagedev/revirth/session"
)

// hostFn — window.jd의 함수. 호스트가 없거나(nop 경로) 함수가 없으면 무효 Value.
func hostFn(name string) js.Value {
	jd := js.Global().Get("jd")
	if !jd.Truthy() {
		return js.Value{}
	}
	f := jd.Get(name)
	if !f.Truthy() {
		return js.Value{}
	}
	return f
}

// statSet — 계측 필드 세팅(호스트의 __jdStatsSet). 없으면 조용히 건너뛴다.
func statSet(name string, v int) {
	f := js.Global().Get("__jdStatsSet")
	if f.Truthy() {
		f.Invoke(name, v)
	}
}

// sharedLogState — 호스트 ?s= 세션 상태(jd.sharedLogReady(): number).
// 0 = GET 진행 중(프레임마다 재확인 대상), 1 = 페이로드 도착, -1 = 없음/실패.
// API가 없으면 -1(없음)로 간주해 폴링이 영원히 도는 것을 막는다(입력 방어).
func sharedLogState() int {
	f := hostFn("sharedLogReady")
	if !f.Truthy() {
		return -1
	}
	v := f.Invoke()
	if v.Type() == js.TypeNumber {
		return v.Int()
	}
	return -1
}

// sharedLog — 호스트가 돌려준 공유 페이로드 문자열(저장소 log 또는 인라인 ?s=v2.…)을
// 디코드한다. 없거나 깨지면 nil(부분 결과는 살린다 — DecodeURL 계약). 도착 성공 시
// 디코드 엔트리 수를 계측에 노출한다(measure 게이트가 열린 페이지에서 재읽는다).
func sharedLog() *session.Log {
	f := hostFn("sharedLog")
	if !f.Truthy() {
		return nil
	}
	v := f.Invoke()
	if v.Type() != js.TypeString || v.String() == "" {
		return nil
	}
	l, _ := session.DecodeURL(v.String())
	if l == nil || len(l.Entries) == 0 {
		return nil
	}
	statSet("sharedEntries", len(l.Entries))
	return l
}

func installShare(g *game) {
	js.Global().Set("jdShareURL", js.FuncOf(func(this js.Value, args []js.Value) any {
		p := js.Global().Get("Promise")
		if !p.Truthy() {
			return "" // 사실상 없는 경로 — 빈 결과로 통일(버튼은 'share n/a')
		}
		if g.in.log == nil || len(g.in.log.Entries) == 0 {
			return p.Call("resolve", "") // 공유할 로그가 없다 — 종전과 같은 빈 결과
		}
		fn := hostFn("shareSession")
		if !fn.Truthy() {
			return p.Call("reject", "share: 호스트 shareSession 없음")
		}
		u := session.EncodeURL(g.in.log) // 감량 0단계 — 저장 페이로드는 무손실(§12.6)
		statSet("shareMirrorEntries", len(g.in.log.Entries))
		if l2, _ := session.DecodeURL(u); l2 != nil {
			// 인코딩 정합 자체 점검(같은 페이로드의 두 번째 디코드) — measure가 열린
			// 페이지의 sharedEntries와 정확 일치를 잰다.
			statSet("shareDecodedEntries", len(l2.Entries))
		}
		return fn.Invoke(u, float64(g.in.log.Seed), g.in.log.Word)
	}))
}
