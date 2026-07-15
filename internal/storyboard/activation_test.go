package storyboard

import (
	"errors"
	"testing"
)

func TestValidateStoryboardPlanActivationAcceptsExactCrossShotCoverage(t *testing.T) {
	timebase := DefaultTimebase()
	shots := []activationShot{
		{ID: "shot-1", StartTick: 0, EndTick: 90_000},
		{ID: "shot-2", StartTick: 90_000, EndTick: 180_000},
	}
	units := []activationTimingUnit{
		{ID: "action", StartTick: 0, EndTick: 60_000},
		{ID: "dialogue", StartTick: 60_000, EndTick: 150_000},
		{ID: "reaction", StartTick: 150_000, EndTick: 180_000},
	}
	spans := []activationTimingSpan{
		{ShotID: "shot-1", UnitID: "action", StartTick: 0, EndTick: 60_000},
		{ShotID: "shot-1", UnitID: "dialogue", StartTick: 60_000, EndTick: 90_000},
		{ShotID: "shot-2", UnitID: "dialogue", StartTick: 90_000, EndTick: 150_000},
		{ShotID: "shot-2", UnitID: "reaction", StartTick: 150_000, EndTick: 180_000},
	}
	if err := validateStoryboardPlanActivation(timebase, 180_000, shots, units, spans); err != nil {
		t.Fatalf("validate exact coverage: %v", err)
	}
}

func TestValidateStoryboardPlanActivationRejectsUnitGap(t *testing.T) {
	timebase := DefaultTimebase()
	shots := []activationShot{
		{ID: "shot-1", StartTick: 0, EndTick: 90_000},
		{ID: "shot-2", StartTick: 90_000, EndTick: 180_000},
	}
	units := []activationTimingUnit{{ID: "dialogue", StartTick: 0, EndTick: 180_000}}
	spans := []activationTimingSpan{
		{ShotID: "shot-1", UnitID: "dialogue", StartTick: 0, EndTick: 90_000},
		{ShotID: "shot-2", UnitID: "dialogue", StartTick: 93_750, EndTick: 180_000},
	}
	err := validateStoryboardPlanActivation(timebase, 180_000, shots, units, spans)
	if !errors.Is(err, ErrStoryboardPlanCoverage) {
		t.Fatalf("error = %v, want coverage error", err)
	}
}

func TestValidateStoryboardPlanActivationRejectsNonFrameBoundary(t *testing.T) {
	timebase := DefaultTimebase()
	shots := []activationShot{{ID: "shot-1", StartTick: 0, EndTick: 90_001}}
	units := []activationTimingUnit{{ID: "action", StartTick: 0, EndTick: 90_000}}
	spans := []activationTimingSpan{{ShotID: "shot-1", UnitID: "action", StartTick: 0, EndTick: 90_000}}
	err := validateStoryboardPlanActivation(timebase, 90_000, shots, units, spans)
	if !errors.Is(err, ErrStoryboardPlanFrameAlignment) && !errors.Is(err, ErrStoryboardPlanCoverage) {
		t.Fatalf("error = %v, want frame alignment or coverage error", err)
	}
}
