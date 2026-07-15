package storyboard

import (
	"fmt"
	"math"
)

const DefaultTimelineTimebase int64 = 90_000

type Timebase struct {
	TicksPerSecond int64 `json:"ticksPerSecond"`
	FPSNumerator   int64 `json:"fpsNumerator"`
	FPSDenominator int64 `json:"fpsDenominator"`
}

func DefaultTimebase() Timebase {
	return Timebase{TicksPerSecond: DefaultTimelineTimebase, FPSNumerator: 24, FPSDenominator: 1}
}

func (t Timebase) Validate() error {
	if t.TicksPerSecond <= 0 || t.FPSNumerator <= 0 || t.FPSDenominator <= 0 {
		return fmt.Errorf("timebase and frame rate must be positive")
	}
	if t.TicksPerSecond*t.FPSDenominator%t.FPSNumerator != 0 {
		return fmt.Errorf("timebase %d cannot represent frame rate %d/%d exactly", t.TicksPerSecond, t.FPSNumerator, t.FPSDenominator)
	}
	return nil
}

func (t Timebase) TicksPerFrame() (int64, error) {
	if err := t.Validate(); err != nil {
		return 0, err
	}
	return t.TicksPerSecond * t.FPSDenominator / t.FPSNumerator, nil
}

func (t Timebase) SecondsToTicks(seconds float64) int64 {
	if seconds <= 0 || t.TicksPerSecond <= 0 {
		return 0
	}
	return int64(math.Round(seconds * float64(t.TicksPerSecond)))
}

func (t Timebase) SecondsToFrameTicksCeil(seconds float64) int64 {
	return t.QuantizeTickCeil(t.SecondsToTicks(seconds))
}

func (t Timebase) TicksToSeconds(ticks int64) float64 {
	if ticks <= 0 || t.TicksPerSecond <= 0 {
		return 0
	}
	return float64(ticks) / float64(t.TicksPerSecond)
}

func (t Timebase) QuantizeTickCeil(tick int64) int64 {
	if tick <= 0 {
		return 0
	}
	frame, err := t.TicksPerFrame()
	if err != nil {
		return tick
	}
	return ((tick + frame - 1) / frame) * frame
}

func (t Timebase) QuantizeTickFloor(tick int64) int64 {
	if tick <= 0 {
		return 0
	}
	frame, err := t.TicksPerFrame()
	if err != nil {
		return tick
	}
	return tick / frame * frame
}

func (t Timebase) QuantizeTickNearest(tick int64) int64 {
	if tick <= 0 {
		return 0
	}
	frame, err := t.TicksPerFrame()
	if err != nil {
		return tick
	}
	return ((tick + frame/2) / frame) * frame
}

func (t Timebase) IsFrameAligned(tick int64) bool {
	frame, err := t.TicksPerFrame()
	return err == nil && tick%frame == 0
}
