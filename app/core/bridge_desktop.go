//go:build !(js && wasm)

package core

import "github.com/midagedev/revirth/engine"

// NewBridge — 데스크톱: 호스트 없음. 전부 no-op(값 0). `go vet ./app/...`용 타입체크 경로.
func NewBridge() Bridge { return nopBridge{} }

type nopBridge struct{}

func (nopBridge) Start()                                   {}
func (nopBridge) Cmd(engine.Cmd, Author)                   {}
func (nopBridge) Tick() Tick                               { return Tick{} }
func (nopBridge) Scope([]byte) bool                        { return false }
func (nopBridge) Param(engine.ParamID) float32             { return 0 }
func (nopBridge) BassStep(engine.Part, int) (uint8, uint8) { return 0, 0 }
func (nopBridge) DrumStep(engine.Part, int) uint8          { return 0 }
func (nopBridge) Muted(engine.Part) bool                   { return false }
func (nopBridge) Slot(engine.Part) uint8                   { return 0 }
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
