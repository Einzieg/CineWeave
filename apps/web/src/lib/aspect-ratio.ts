const DEFAULT_CSS_ASPECT_RATIO = "16 / 9";

export function cssAspectRatio(value?: string): string {
  const match = /^\s*(\d+(?:\.\d+)?)\s*[:/]\s*(\d+(?:\.\d+)?)\s*$/.exec(value ?? "");
  if (!match) return DEFAULT_CSS_ASPECT_RATIO;

  const width = Number(match[1]);
  const height = Number(match[2]);
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return DEFAULT_CSS_ASPECT_RATIO;
  }
  return `${width} / ${height}`;
}
