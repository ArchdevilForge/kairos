import type { IChartApi, ISeriesApi, MouseEventParams, Time } from 'lightweight-charts';
import type { Bar } from '../types/kairos';
import { findBarByTime } from '../utils/chartMagnet';

export const RIGHT_PRICE_SCALE_MIN_WIDTH = 72;

export interface SyncChartHandle {
  id: string;
  chart: IChartApi;
  series: ISeriesApi<'Candlestick'>;
  getBars: () => readonly Bar[];
  getTimeframeSec: () => number;
  requestHistoryBefore?: (tsSec: number) => void;
}

/**
 * Convert a possibly fractional logical index (including left/right whitespace)
 * into a Unix-second timestamp on the given candle series.
 */
export function logicalToTimestamp(
  bars: readonly Bar[],
  tfSec: number,
  logicalIdx: number,
): number | null {
  if (bars.length === 0 || !Number.isFinite(logicalIdx)) return null;
  const step = tfSec > 0 ? tfSec : 300;
  if (logicalIdx <= 0) {
    return bars[0].time + logicalIdx * step;
  }
  const lastIdx = bars.length - 1;
  if (logicalIdx >= lastIdx) {
    return bars[lastIdx].time + (logicalIdx - lastIdx) * step;
  }
  const lo = Math.floor(logicalIdx);
  const hi = Math.min(lastIdx, lo + 1);
  const frac = logicalIdx - lo;
  return bars[lo].time + frac * (bars[hi].time - bars[lo].time);
}

/**
 * Inverse of logicalToTimestamp: map a Unix-second timestamp onto a target
 * chart's fractional logical coordinate space (supporting whitespace extrapolation).
 */
export function timestampToLogical(
  bars: readonly Bar[],
  tfSec: number,
  tsSec: number,
): number | null {
  if (bars.length === 0 || !Number.isFinite(tsSec)) return null;
  const step = tfSec > 0 ? tfSec : 300;
  if (tsSec <= bars[0].time) {
    return (tsSec - bars[0].time) / step;
  }
  const lastIdx = bars.length - 1;
  if (tsSec >= bars[lastIdx].time) {
    return lastIdx + (tsSec - bars[lastIdx].time) / step;
  }

  let lo = 0;
  let hi = lastIdx;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (bars[mid].time <= tsSec) lo = mid + 1;
    else hi = mid - 1;
  }
  const idx = Math.max(0, Math.min(lastIdx - 1, hi));
  const span = bars[idx + 1].time - bars[idx].time || step;
  return idx + (tsSec - bars[idx].time) / span;
}

/**
 * Synchronize visible time range (via fractional logical <-> timestamp mapping so
 * whitespace and cross-timeframe ratios remain locked) and crosshair across charts.
 */
export function syncChartGroup(handles: SyncChartHandle[]): () => void {
  if (handles.length < 2) {
    return () => {};
  }

  let activeSourceId: string | null = null;
  const cleanups: Array<() => void> = [];

  const propagateRange = (source: SyncChartHandle) => {
    const srcBars = source.getBars();
    if (srcBars.length === 0) return;
    let srcLogical: { from: number; to: number } | null = null;
    try {
      srcLogical =
        (source.chart.timeScale().getVisibleLogicalRange() as {
          from: number;
          to: number;
        } | null) ?? null;
    } catch {
      return;
    }
    if (!srcLogical || !Number.isFinite(srcLogical.from) || !Number.isFinite(srcLogical.to)) {
      return;
    }

    const srcTf = source.getTimeframeSec();
    const fromTs = logicalToTimestamp(srcBars, srcTf, srcLogical.from);
    const toTs = logicalToTimestamp(srcBars, srcTf, srcLogical.to);
    if (fromTs == null || toTs == null || toTs <= fromTs) return;

    activeSourceId = source.id;
    try {
      for (const target of handles) {
        if (target.id === source.id) continue;
        const targetBars = target.getBars();
        if (targetBars.length === 0) continue;
        const targetTf = target.getTimeframeSec();
        const tFrom = timestampToLogical(targetBars, targetTf, fromTs);
        const tTo = timestampToLogical(targetBars, targetTf, toTs);
        if (tFrom == null || tTo == null || tTo <= tFrom) continue;

        try {
          target.chart.timeScale().setVisibleLogicalRange({ from: tFrom, to: tTo });
        } catch {
          // Ignore transient disposal or unmounted chart state.
        }

        if (fromTs < targetBars[0].time + targetTf * 30) {
          target.requestHistoryBefore?.(targetBars[0].time);
        }
      }
    } finally {
      activeSourceId = null;
    }
  };

  for (const source of handles) {
    const onVisibleLogicalRangeChange = () => {
      if (activeSourceId !== null && activeSourceId !== source.id) return;
      propagateRange(source);
    };

    const onCrosshairMove = (param: MouseEventParams<Time>) => {
      if (activeSourceId !== null && activeSourceId !== source.id) return;
      activeSourceId = source.id;
      try {
        if (param.time == null || !param.point) {
          for (const target of handles) {
            if (target.id === source.id) continue;
            try {
              target.chart.clearCrosshairPosition();
            } catch {
              // Ignore disposed target.
            }
          }
          return;
        }

        const timeSec = typeof param.time === 'number' ? param.time : Number(param.time);
        if (!Number.isFinite(timeSec)) return;

        for (const target of handles) {
          if (target.id === source.id) continue;
          const targetBars = target.getBars();
          const bar = findBarByTime(targetBars, timeSec, false);
          if (!bar) {
            try {
              target.chart.clearCrosshairPosition();
            } catch {
              // Ignore.
            }
            continue;
          }
          try {
            target.chart.setCrosshairPosition(
              bar.close,
              bar.time as Time,
              target.series,
            );
          } catch {
            // Ignore when target series has no plotted point at timestamp.
          }
        }
      } finally {
        activeSourceId = null;
      }
    };

    source.chart.timeScale().subscribeVisibleLogicalRangeChange(onVisibleLogicalRangeChange);
    source.chart.subscribeCrosshairMove(onCrosshairMove);

    cleanups.push(() => {
      try {
        source.chart
          .timeScale()
          .unsubscribeVisibleLogicalRangeChange(onVisibleLogicalRangeChange);
        source.chart.unsubscribeCrosshairMove(onCrosshairMove);
      } catch {
        // Chart already disposed.
      }
    });
  }

  // Align comparison chart immediately to the primary chart on mount.
  propagateRange(handles[0]);

  return () => {
    for (const fn of cleanups) fn();
  };
}
