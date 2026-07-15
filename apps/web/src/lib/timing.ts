export const DEFAULT_TIMELINE_TIMEBASE = 90_000;
export const DEFAULT_FPS_NUMERATOR = 24;
export const DEFAULT_FPS_DENOMINATOR = 1;

export type FrameTimebase = {
  timelineTimebase?: number | null;
  fpsNumerator?: number | null;
  fpsDenominator?: number | null;
};

export function normalizeFrameTimebase(value?: FrameTimebase | null) {
  const timelineTimebase = positiveInteger(value?.timelineTimebase) || DEFAULT_TIMELINE_TIMEBASE;
  const fpsNumerator = positiveInteger(value?.fpsNumerator) || DEFAULT_FPS_NUMERATOR;
  const fpsDenominator = positiveInteger(value?.fpsDenominator) || DEFAULT_FPS_DENOMINATOR;
  const ticksPerFrame = timelineTimebase * fpsDenominator / fpsNumerator;
  if (!Number.isInteger(ticksPerFrame) || ticksPerFrame <= 0) {
    return {
      timelineTimebase: DEFAULT_TIMELINE_TIMEBASE,
      fpsNumerator: DEFAULT_FPS_NUMERATOR,
      fpsDenominator: DEFAULT_FPS_DENOMINATOR,
      ticksPerFrame: DEFAULT_TIMELINE_TIMEBASE / DEFAULT_FPS_NUMERATOR,
    };
  }
  return { timelineTimebase, fpsNumerator, fpsDenominator, ticksPerFrame };
}

export function secondsToFrameTicks(seconds: number, value?: FrameTimebase | null) {
  if (!Number.isFinite(seconds) || seconds <= 0) return 0;
  const timebase = normalizeFrameTimebase(value);
  const frameCount = Math.ceil(seconds * timebase.fpsNumerator / timebase.fpsDenominator);
  return frameCount * timebase.ticksPerFrame;
}

export function ticksToSeconds(ticks: number | null | undefined, value?: FrameTimebase | null) {
  if (!Number.isFinite(ticks) || !ticks || ticks <= 0) return 0;
  return ticks / normalizeFrameTimebase(value).timelineTimebase;
}

export function wholeSecondDuration(seconds: number | null | undefined) {
  if (!Number.isFinite(seconds) || Number(seconds) <= 0) return 0;
  return Math.max(1, Math.ceil(Number(seconds) - 1e-9));
}

function positiveInteger(value: number | null | undefined) {
  return Number.isInteger(value) && Number(value) > 0 ? Number(value) : 0;
}
