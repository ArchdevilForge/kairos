import type { Bar } from '../types/kairos';

/** Screen pixel tolerance within which cursor snaps to nearest bar OHLC. */
export const MAGNET_TOLERANCE_PX = 8;

export type MagnetSnap = {
  price: number;
  snapped: boolean;
};

/**
 * Binary-search the bar whose timestamp equals `timeSec` (or nearest <= `timeSec` if exact=false).
 */
export function findBarByTime(
  bars: readonly Bar[],
  timeSec: number,
  exact = true,
): Bar | null {
  if (bars.length === 0 || !Number.isFinite(timeSec)) return null;
  let lo = 0;
  let hi = bars.length - 1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    const t = bars[mid].time;
    if (t === timeSec) return bars[mid];
    if (t < timeSec) lo = mid + 1;
    else hi = mid - 1;
  }
  if (exact) return null;
  return hi >= 0 ? bars[hi] : bars[0];
}

/**
 * Snap cursor price to the closest OHLC value of `bar` within `tolerance` screen pixels.
 */
export function snapPriceToOhlc(
  price: number,
  bar: Pick<Bar, 'open' | 'high' | 'low' | 'close'> | null,
  toScreen: (price: number) => number | null,
  cursorY: number,
  tolerance = MAGNET_TOLERANCE_PX,
): MagnetSnap {
  if (!bar) return { price, snapped: false };
  let best = price;
  let bestDist = Number.POSITIVE_INFINITY;
  for (const cand of [bar.open, bar.high, bar.low, bar.close]) {
    const cy = toScreen(cand);
    if (cy == null || !Number.isFinite(cy)) continue;
    const d = Math.abs(cy - cursorY);
    if (d <= tolerance && d < bestDist) {
      bestDist = d;
      best = cand;
    }
  }
  return bestDist === Number.POSITIVE_INFINITY
    ? { price, snapped: false }
    : { price: best, snapped: true };
}
