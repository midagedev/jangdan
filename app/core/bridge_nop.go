// bridge_nop.go — 호스트가 없을 때의 Bridge 대체 구현. **빌드 태그가 없다**: 데스크톱과
// js 양쪽이 같은 한 벌을 쓴다.
//
// 왜 한 벌인가(2026-09-06 사건): 이 타입이 bridge_desktop.go와 bridge_js.go에 각각 있었다.
// Bridge에 메서드를 하나 더할 때 `go vet ./app/...`(데스크톱 태그)는 통과하는데 wasm 빌드만
// 깨진다 — 컴파일이 브라우저 빌드 시점까지 미뤄지는 부류의 결함이다. 사본을 없애 그 클래스를 닫는다.
package core

import "github.com/midagedev/jangdan/engine"

type nopBridge struct{}

func (nopBridge) Start()                                   {}
func (nopBridge) Cmd(engine.Cmd, Author)                   {}
func (nopBridge) Tick() Tick                               { return Tick{} }
func (nopBridge) Scope([]byte) bool                        { return false }
func (nopBridge) Param(engine.ParamID) float32             { return 0 }
func (nopBridge) DevParam(int, int) float32                { return -1 }
func (nopBridge) RackRev() uint32                          { return 0 }
func (nopBridge) RackKind(int) engine.DeviceKind           { return engine.KindNone }
func (nopBridge) Cables([]RackCable) int                   { return 0 }
func (nopBridge) BassStep(engine.Part, int) (uint8, uint8) { return 0, 0 }
func (nopBridge) DrumStep(engine.Part, int) uint8          { return 0 }
func (nopBridge) Muted(engine.Part) bool                   { return false }
func (nopBridge) Slot(engine.Part) uint8                   { return 0 }
func (nopBridge) KeyRoot() int                             { return 0 }
func (nopBridge) Chord(int) (uint8, uint8)                 { return 0, 0 }
func (nopBridge) Mode(engine.Part) (uint8, uint8)          { return 0, 0 }
func (nopBridge) Hint(int)                                 {}
func (nopBridge) Telemetry(string, float64)                {}
func (nopBridge) Replay(float64)                           {}
func (nopBridge) SeedWord() string                         { return "" }
func (nopBridge) ReducedMotion() bool                      { return false }
func (nopBridge) Hidden() bool                             { return false }
func (nopBridge) CleanScreen() bool                        { return false }
func (nopBridge) WallClock() (int, int, int)               { return 0, 0, 0 }
func (nopBridge) Frame(float64)                            {}
func (nopBridge) FirstFrame()                              {}
func (nopBridge) AllocPerFrame(float64)                    {}
