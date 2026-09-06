// knobs.go — 노브 상태·히트·드래그·탭 스윕. 좌표는 전부 레이아웃 JSON에서 온다.
//
// 값 소스 계약: 잡히지 않은 노브는 매 프레임 Bridge.Param(레지던트·리플레이가 움직인 값),
// 잡히거나 스윕 중인 노브는 로컬 값. SetParam은 값이 바뀔 때만, 노브당 프레임마다 최대 1개.
//
// 장치 로컬 노브(P5-poly, §14.1): 전역 ParamID가 아니라 (슬롯, k) 장치 파라미터를 움직인다.
// dev 분기는 knobValue·sendParam 두 곳에만 있다 — 히트·드래그·스윕 상태기계는 전역 노브와
// 같은 코드를 공유한다(구조 봉쇄: dev 가지가 흩어지지 않게).
package device

import (
	"math"
	"strings"

	"github.com/midagedev/jangdan/app/core"
	"github.com/midagedev/jangdan/engine"
)

// knob — 레이아웃 노브 하나의 정적 기하 + 실행 상태.
type knob struct {
	name      string
	label     string // 패널에 그리는 표시명(드럼·믹서·fx2는 내부명과 다르다 — knobLabel)
	sec       uint8  // secBassA..secPoly
	cx, cy, r float64
	id        engine.ParamID

	// 장치 로컬 파라미터(P5-poly — §14.1 DeviceParam). dev=false면 id가, dev=true면
	// (slot, k)가 이 노브의 값 소스·송신 대상이다(id는 쓰는 경로가 없다).
	dev  bool
	slot int
	k    int

	held     bool    // 이번 순간 포인터에 잡힘(그리기 밝기)
	useLocal bool    // 잡힘 또는 스윕 중 → 표시값은 local
	local    float32 // 로컬 값
	lastSent float32 // 마지막으로 송신한 값(무변화 시 무송신)

	swActive  bool
	swT0      float64 // 스윕 시작 시각(ctx.Now)
	swV0      float32 // 시작==복귀값
	swPeak    float32 // min(1, v0+0.5)
	swBar     float64 // 1바 길이(초) — 템포 미러에서 계산
	swLastT   float64 // 마지막 송신 시각
	swLastVal float32 // 마지막 송신 값
	swSent    bool    // 스윕 중 송신 1회 이상
}

// knobLabel — 노브 위 라벨 표시명. 드럼 노브의 내부 파라미터명(BD_LEVEL)은 보이스명까지
// 품고 있어 그대로 올리면 내부명이 노출된다 — 표시명(LEVEL)만 쓰고 보이스명은 패드 라벨이
// 담당한다(비전 판정 2026-09-05 처방). 믹서·fx2(§13.3)는 버스 접두(REV_·CHO_)를 떼고
// 나머지 밑줄은 공백(REV_BD→"BD", LEVEL_A→"LEVEL A", CHO_RATE→"RATE").
// 폴리(P5-poly)는 노브 피치 80px의 r25 라벨판 폭에 맞춘 3~4자 축약 표.
// 나머지 섹션은 레이아웃 이름 그대로. 구성 시 1회라 무할당 규칙 밖이다.
func knobLabel(sec uint8, name string) string {
	switch sec {
	case secDrums:
		if i := strings.IndexByte(name, '_'); i >= 0 {
			return name[i+1:]
		}
	case secMixer, secFx2:
		n := strings.TrimPrefix(strings.TrimPrefix(name, "REV_"), "CHO_")
		return strings.ReplaceAll(n, "_", " ")
	case secPoly:
		switch name {
		case "CUTOFF":
			return "CUT"
		case "ATTACK":
			return "ATK"
		case "DECAY":
			return "DEC"
		case "RELEASE":
			return "REL"
		case "DETUNE":
			return "DET"
		case "LEVEL":
			return "LVL"
		}
	}
	return name
}

// hitKnob — 중심 거리 ≤ r+hitKnobPad. 여러 개가 겹치면 가장 가까운 것.
func (v *View) hitKnob(x, y float64) int {
	best, bestD := -1, math.MaxFloat64
	for i := range v.knobs {
		k := &v.knobs[i]
		dx, dy := x-k.cx, y-k.cy
		d := dx*dx + dy*dy
		lim := k.r + hitKnobPad
		if d <= lim*lim && d < bestD {
			best, bestD = i, d
		}
	}
	return best
}

// knobValue — 이 프레임의 표시값(그리기·표시창 공용).
func (v *View) knobValue(ctx *core.Ctx, k *knob) float32 {
	if k.useLocal {
		return k.local
	}
	if k.dev {
		// 장치 로컬 미러(§14.1): 음수 = 미러 부재 → 기본값 폴백. NaN도 무신호로 폴백,
		// 1 초과는 클램프 — 표시값은 어떤 브리지에서든 0..1(3클래스 입력 방어).
		val := ctx.Bridge.DevParam(k.slot, k.k)
		if val != val || val < 0 {
			return core.DevParamDefault(k.slot, k.k)
		}
		if val > 1 {
			return 1
		}
		return val
	}
	return ctx.Bridge.Param(k.id)
}

// grabKnob — 포인터가 노브를 잡는 순간. 스윕은 취소되고 잡은 시점의 표시값부터 드래그한다.
func (v *View) grabKnob(ctx *core.Ctx, k int) {
	kn := &v.knobs[k]
	val := v.knobValue(ctx, kn) // useLocal을 켜기 전에 읽는다(브리지 미러)
	kn.swActive = false
	kn.held = true
	kn.useLocal = true
	kn.local = val
	kn.lastSent = val
	// JustGrabbed는 전역 ParamID(MANUAL 잠금 대상)를 보고한다 — 장치 로컬 노브는 id가
	// 무의미하므로 보고하지 않는다(res.Lock(0)이 베이스 A TUNE을 거짓 잠금하는 부류 봉쇄).
	// 장치 로컬 잠금 id는 코어 확정 과제(보고서 참조).
	if !v.grabOK && !kn.dev {
		v.grabID, v.grabOK = kn.id, true
	}
	if kn.sec <= secBassB {
		v.disp[kn.sec].knob = k
		v.disp[kn.sec].knobT = ctx.Now // B 표시창: 노브 접촉 시각(2초 뒤 모드 문자열 복귀)
		v.disp[kn.sec].val99 = -1      // 다음 cacheDisplays에서 재구성
	}
}

// releaseKnob — 놓을 때 이동 < tapMoveMax && 눌림 < tapDurMax 이면 2바 자동 스윕.
// 바 길이는 Tick의 bar 변화가 아니라 템포 미러(60/BPM×4초)에서 계산한다.
func (v *View) releaseKnob(ctx *core.Ctx, k int, moved, dur float64) {
	kn := &v.knobs[k]
	kn.held = false
	if moved < tapMoveMax && dur < tapDurMax {
		kn.swActive = true
		kn.swT0 = ctx.Now
		kn.swV0 = kn.local
		kn.swPeak = clamp01(kn.local + 0.5)
		kn.swBar = 4 * 60 / engine.BPMOf(ctx.Bridge.Param(engine.Tempo))
		kn.swLastT = ctx.Now
		kn.swLastVal = kn.local
		kn.swSent = false
		return
	}
	if !kn.swActive {
		kn.useLocal = false
	}
}

// runSweeps — 매 프레임: 상승 1바 → 복귀 1바(선형). 송신 간격 ≥ sweepSendMin,
// 마지막 송신은 정확히 시작값(엔진 양자화 오차 0 기준 ±1/4095 이내 보장).
func (v *View) runSweeps(ctx *core.Ctx) {
	for i := range v.knobs {
		k := &v.knobs[i]
		if !k.swActive {
			continue
		}
		t := ctx.Now - k.swT0
		if t < 0 {
			t = 0
		}
		if t >= 2*k.swBar {
			// 복귀 완료: 간격을 지켜 시작값을 정확히 송신하고 끝낸다.
			if k.swSent && ctx.Now-k.swLastT < sweepSendMin {
				continue
			}
			if k.swLastVal != k.swV0 {
				v.sendParam(ctx, k, k.swV0)
			}
			k.swActive = false
			if !k.held {
				k.useLocal = false
			}
			continue
		}
		ph := t / k.swBar
		if ph > 1 {
			ph = 1 - (t-k.swBar)/k.swBar
		}
		val := clamp01(float32(float64(k.swV0) + float64(k.swPeak-k.swV0)*ph))
		k.local = val
		if !k.swSent || ctx.Now-k.swLastT >= sweepSendMin {
			if val != k.swLastVal {
				v.sendParam(ctx, k, val)
				k.swLastT, k.swLastVal, k.swSent = ctx.Now, val, true
			}
		}
	}
}

// sendParam — 값 송신(노브당 프레임 최대 1회는 호출부가 보장). 전역 노브는 SetParam,
// 장치 로컬 노브(P5-poly)는 DeviceParam(A=슬롯, B=k) — 드래그·탭 스윕 모두 이 경로뿐이다.
func (v *View) sendParam(ctx *core.Ctx, k *knob, val float32) {
	if k.dev {
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.DeviceParam, A: uint8(k.slot), B: uint8(k.k), V: val}, core.Human)
	} else {
		ctx.Bridge.Cmd(engine.Cmd{Kind: engine.SetParam, A: uint8(k.id), V: val}, core.Human)
	}
	k.lastSent = val
}

func clamp01(x float32) float32 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}
