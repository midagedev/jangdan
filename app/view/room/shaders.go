// shaders.go — Kage 셰이더 3종(비·스탠드 빛·먼지) 소스와 컴파일. 계약: 스펙 §15~§17.
//
// 유니폼은 v2.9 Kage 문법대로 대문자 전역 var(초기값 금지). 반환이든 ColorScale이든
// 프리멀티 알파로 쓴다(v2.9 블렌드가 ONE/ONE_MINUS_SRC_ALPHA). 좌표는 비·스탠드가
// 절대 화면 좌표(dstPos.xy — 레이아웃 좌표와 같은 공간), 먼지는 imageDstOrigin()을 뺀
// 사각형 지역 좌표(파티클이 [0,Size] 안에서 생성된다). 유니폼 맵은 New에서 한 번 만들고
// 값을 제자리 갱신해 프레임당 할당을 0으로 둔다(스칼라 대입의 박싱 몇 개 제외).
package room

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
)

// rainSrc — 천창의 비. 열 단위 해시로 줄기 위치·위상, 아래로 900px/s, 길이 40~80px.
// Density(0..1)가 보이는 열의 비율, Thickness(px)가 줄기 굵기, 알파 상한 0.6.
// colW 6·span 140은 커버리지 실측으로 정했다(기본 패턴 밀도에서 바꾼 픽셀 비율
// ≈ 0.04 — 비가 "보인다"는 시각 게이트 하한 0.03을 여유 있게 넘는다.
// span은 900px/s·8s=7200과 겹치지 않게: 7200 mod 140 = 60이라 8초 뒤 위상이
// 완전히 달라진다).
const rainSrc = `package main

var Time float
var Density float
var Thickness float
var Color vec4

func hash(n float) float {
	return fract(sin(n)*43758.5453)
}

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	pos := dstPos.xy
	colW := 6.0
	c := floor(pos.x / colW)
	h1 := hash(c*1.13 + 3.7)
	if h1 > Density {
		return vec4(0)
	}
	h2 := hash(c*2.17 + 9.1)
	h3 := hash(c*3.71 + 1.3)
	h4 := hash(c*4.53 + 7.9)
	cx := (c + 0.2 + 0.6*h2) * colW
	span := 140.0
	head := mod(h3*span + Time*900.0, span)
	streak := 40.0 + 40.0*h4
	rel := mod(head - pos.y, span)
	if rel > streak {
		return vec4(0)
	}
	dx := abs(pos.x - cx)
	w := clamp(Thickness*0.5 + 0.5 - dx, 0.0, 1.0)
	fade := 1.0 - rel/streak
	a := Color.a * w * fade
	return vec4(Color.rgb * a, a)
}
`

// lampSrc — 스탠드 빛 풀. 방사형 감쇠 (1−d/R)^2 × 밝기 × 0.35 알파. 가산(BlendLighter)이
// 아니라 일반 알파 — 글로우·블룸 금지, 무광 룩(기획서). 밝기 상한은 기본 1.0 + 호흡 6%.
const lampSrc = `package main

var Center vec2
var Radius float
var Brightness float
var Color vec4

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	d := length(dstPos.xy - Center)
	f := clamp(1.0 - d/Radius, 0.0, 1.0)
	a := f * f * 0.35 * Brightness
	return vec4(Color.rgb * Color.a * a, Color.a * a)
}
`

// dustSrc — 등빛 원뿔의 먼지. 파티클 12개(셰이더 안 해시), 위로 6px/s, 크기 1.5px, 알파 0.25.
const dustSrc = `package main

var Time float
var Size vec2
var Color vec4

func hash(n float) float {
	return fract(sin(n)*43758.5453)
}

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	pos := dstPos.xy - imageDstOrigin()
	a := 0.0
	for i := 0.0; i < 12.0; i += 1.0 {
		px := hash(i*3.7 + 1.3) * Size.x
		py := mod(hash(i*7.9+2.9)*Size.y - Time*6.0, Size.y)
		d := length(vec2(pos.x-px, pos.y-py))
		a += clamp(1.25 - d, 0.0, 1.0)
	}
	a = clamp(a, 0.0, 1.0)
	return vec4(Color.rgb * Color.a * a * 0.25, Color.a * a * 0.25)
}
`

// shaderSet — 컴파일된 셰이더 3종과 재사용 유니폼 맵·드로우 옵션. 값 갱신은 제자리(맵 키
// 불변). 옵션의 Uniforms는 생성 시 1회만 바인딩한다 — 드로우 경로에서 대입을 빠뜨리면
// v2.9가 빠진 유니폼을 전부 0으로 채운다(실측 결함: Density 0 → 비 전멸, Radius 0 →
// 램프 빛 소멸). 생성 시 못박으면 잊을 경로 자체가 없다.
type shaderSet struct {
	rain, lamp, dust *ebiten.Shader
	rainU            map[string]any
	lampU            map[string]any
	dustU            map[string]any

	rainShop, lampShop, dustShop ebiten.DrawRectShaderOptions // Uniforms 생성 시 바인드

	rainColor  []float32 // [r,g,b,a] — 제자리 갱신
	lampColor  []float32
	lampCenter []float32
	dustColor  []float32
	dustSize   []float32
}

func newShaders() (*shaderSet, error) {
	s := &shaderSet{
		rainColor:  make([]float32, 4),
		lampColor:  make([]float32, 4),
		lampCenter: make([]float32, 2),
		dustColor:  make([]float32, 4),
		dustSize:   make([]float32, 2),
	}
	var err error
	if s.rain, err = ebiten.NewShader([]byte(rainSrc)); err != nil {
		return nil, fmt.Errorf("room: rain shader: %w", err)
	}
	if s.lamp, err = ebiten.NewShader([]byte(lampSrc)); err != nil {
		return nil, fmt.Errorf("room: lamp shader: %w", err)
	}
	if s.dust, err = ebiten.NewShader([]byte(dustSrc)); err != nil {
		return nil, fmt.Errorf("room: dust shader: %w", err)
	}
	s.rainU = map[string]any{"Time": float32(0), "Density": float32(0.15), "Thickness": float32(1), "Color": s.rainColor}
	s.lampU = map[string]any{"Center": s.lampCenter, "Radius": float32(1), "Brightness": float32(1), "Color": s.lampColor}
	s.dustU = map[string]any{"Time": float32(0), "Size": s.dustSize, "Color": s.dustColor}
	s.rainShop.Uniforms = s.rainU
	s.lampShop.Uniforms = s.lampU
	s.dustShop.Uniforms = s.dustU
	return s, nil
}
