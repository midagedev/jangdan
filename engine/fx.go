// fx.go — 이펙트 체인(T1c 소유 파일). engine.go가 호출하는 메서드 집합이 계약이다:
//
//	init()                              New/Reset — 버퍼 0, 계수 기본
//	setParam(k int, q float32)          k 0=Delay 1=Drive 2=Comp 3=Master
//	setTempo(samplesPerStep float64)    딜레이 시간(점8분 = 6스텝) 재계산
//	process(bass, drums, bdSide, dropDrive float32) (l, r float32)
//	    체인: 베이스 사이드체인 덕킹(bdSide 트리거) → 드라이브(+dropDrive·0.2) → 템포 딜레이(핑퐁) → 마스터 → 소프트클립
//
// 딜레이 버퍼는 이 구조체 안의 고정 배열이다(New 밖 할당 0). 현재 내용은 스탠드인(마스터+
// 소프트클립만). T1c 라운드가 체인 전체를 구현한다(docs/impl-plan-2026-09-05.md §2.6).
// 이 파일의 곱셈-덧셈은 전부 mul32(a,b)+z 꼴.
package engine

const delayBufLen = SampleRate // 1초 × 채널

type fxChain struct {
	bufL, bufR [delayBufLen]float32
	wpos       int
	delaySamp  int
	delayMix   float32
	delayFB    float32
	drive      float32
	comp       float32
	master     float32
	duck       float32
}

func (f *fxChain) init() {
	*f = fxChain{master: 0.8, delaySamp: 24000}
}

func (f *fxChain) setParam(k int, q float32) {
	switch k {
	case 0:
		f.delayMix = mul32(q, 0.5)
		f.delayFB = mul32(q, 0.7)
	case 1:
		f.drive = q
	case 2:
		f.comp = q
	case 3:
		f.master = q
	}
}

func (f *fxChain) setTempo(samplesPerStep float64) {
	n := int(samplesPerStep * 6) // 점8분
	if n >= delayBufLen {
		n = delayBufLen - 1
	}
	if n < 1 {
		n = 1
	}
	f.delaySamp = n
}

func (f *fxChain) process(bass, drums, bdSide, dropDrive float32) (float32, float32) {
	m := bass + drums
	if m > 8 {
		m = 8
	}
	if m < -8 {
		m = -8
	}
	x2 := mul32(m, m)
	num := mul32(m, 27+x2)
	den := mul32(9, x2) + 27
	y := num / den
	o := mul32(y, 0.5)
	o = mul32(o, f.master)
	return o, o
}
