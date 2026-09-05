// shaders.go — Kage 셰이더 3종(비·스탠드 빛·먼지) 소스와 컴파일. 계약: 스펙 §15~§17.
//
// 유니폼은 v2.9 Kage 문법대로 대문자 전역 var(초기값 금지). 좌표는 dstPos에서
// imageDstOrigin()을 빼 그리기 사각형 기준 지역 좌표로 쓴다 — 사각형이 정적이므로
// 열 해시(비)·파티클(먼지)의 결정론이 유지된다. 유니폼 맵은 New에서 한 번 만들고
// 값을 제자리 갱신해 프레임당 할당을 0으로 둔다(스칼라 대입의 박싱 몇 개 제외).
package room

import (
	"fmt"

	"github.com/hajimehoshi/ebiten/v2"
)

// rainSrc — 천창의 비. 열 단위 해시로 줄기 위치·위상, 아래로 900px/s, 길이 40~80px.
// Density(0..1)가 보이는 열의 비율, Thickness(px)가 줄기 굵기, 알파 상한 0.6.
const rainSrc = `package main

var Time float
var Density float
var Thickness float
var Color vec4

func hash(n float) float {
	return fract(sin(n)*43758.5453)
}

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	pos := dstPos.xy - imageDstOrigin()
	colW := 18.0
	c := floor(pos.x / colW)
	h1 := hash(c*1.13 + 3.7)
	h2 := hash(c*2.17 + 9.1)
	h3 := hash(c*3.71 + 1.3)
	h4 := hash(c*4.53 + 7.9)
	if h1 > Density {
		return vec4(0)
	}
	cx := (c + 0.2 + 0.6*h2) * colW
	span := 760.0
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
	return vec4(Color.rgb, a)
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
	p := dstPos.xy - imageDstOrigin()
	d := length(p - Center)
	f := clamp(1.0 - d/Radius, 0.0, 1.0)
	a := f * f * 0.35 * Brightness
	return vec4(Color.rgb, Color.a * a)
}
`

// dustSrc — 등빛 원뿔의 먼지. 파티클 12개(셰이더 안 해시), 위로 6px/s, 크기 1.5px, 알파 0.25.
const dustSrc = `package main

var Time float
var Color vec4

func hash(n float) float {
	return fract(sin(n)*43758.5453)
}

func Fragment(dstPos vec4, srcPos vec2, color vec4) vec4 {
	pos := dstPos.xy - imageDstOrigin()
	size := imageDstSize()
	a := 0.0
	for i := 0.0; i < 12.0; i += 1.0 {
		px := hash(i*3.7 + 1.3) * size.x
		py := mod(hash(i*7.9+2.9)*size.y - Time*6.0, size.y)
		d := length(vec2(pos.x-px, pos.y-py))
		a += clamp(1.25 - d, 0.0, 1.0)
	}
	a = clamp(a, 0.0, 1.0)
	return vec4(Color.rgb, Color.a * a * 0.25)
}
`

// shaderSet — 컴파일된 셰이더 3종과 재사용 유니폼 맵. 값 갱신은 제자리(맵 키 불변).
type shaderSet struct {
	rain, lamp, dust *ebiten.Shader
	rainU            map[string]any
	lampU            map[string]any
	dustU            map[string]any

	rainColor  []float32 // [r,g,b,a] — 제자리 갱신
	lampColor  []float32
	lampCenter []float32
	dustColor  []float32
}

func newShaders() (*shaderSet, error) {
	s := &shaderSet{
		rainColor:  make([]float32, 4),
		lampColor:  make([]float32, 4),
		lampCenter: make([]float32, 2),
		dustColor:  make([]float32, 4),
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
	s.dustU = map[string]any{"Time": float32(0), "Color": s.dustColor}
	return s, nil
}
