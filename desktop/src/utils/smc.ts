import type { Bar } from '../types/kairos';

export type SmcDirection = 'bull' | 'bear';
export type SmcTier = 'swing' | 'internal';

export interface SmcStructureBreak {
  kind: 'BOS' | 'CHoCH';
  tier: SmcTier;
  direction: SmcDirection;
  level: number;
  startTime: number;
  endTime: number;
}

export interface SmcOrderBlock {
  tier: SmcTier;
  direction: SmcDirection;
  top: number;
  bottom: number;
  startTime: number;
}

export interface SmcEqualLevel {
  kind: 'EQH' | 'EQL';
  level: number;
  startTime: number;
  endTime: number;
}

export interface SmcOverlayData {
  breaks: SmcStructureBreak[];
  orderBlocks: SmcOrderBlock[];
  equalLevels: SmcEqualLevel[];
}

interface ActivePivot {
  index: number;
  time: number;
  level: number;
  crossed: boolean;
}

interface InternalOrderBlock extends SmcOrderBlock {
  createdBreakIdx: number;
}

interface InternalEqualLevel extends SmcEqualLevel {
  sweepBound: number;
  confirmedIdx: number;
}

const SWING_LENGTH = 50;
const INTERNAL_LENGTH = 5;
const EQ_LENGTH = 3;
const EQ_ATR_THRESHOLD = 0.1;
const ATR_PERIOD = 200;
const MAX_ACTIVE_ORDER_BLOCKS = 5;
const MAX_SWING_ORDER_BLOCKS = 3;
const MAX_SWING_BREAKS = 8;
const MAX_INTERNAL_BREAKS = 16;
const MAX_EQUAL_LEVELS = 3;

/**
 * Causal Wilder ATR(200) matching LuxAlgo's volatility filter without lookahead.
 */
function computeCausalAtr(bars: readonly Bar[]): Float64Array {
  const atr = new Float64Array(bars.length);
  let sumTr = 0;
  for (let i = 0; i < bars.length; i += 1) {
    const b = bars[i];
    const tr =
      i === 0
        ? b.high - b.low
        : Math.max(
            b.high - b.low,
            Math.abs(b.high - bars[i - 1].close),
            Math.abs(b.low - bars[i - 1].close),
          );
    if (i < ATR_PERIOD) {
      sumTr += tr;
      atr[i] = sumTr / (i + 1);
    } else {
      atr[i] = (atr[i - 1] * (ATR_PERIOD - 1) + tr) / ATR_PERIOD;
    }
  }
  return atr;
}

/**
 * Shared causal leg(size) state check at index i evaluating candidate pivot p = i - size.
 * Returns 0 when candidate is a strict local high, 1 when strict local low, or prevLeg otherwise.
 */
function nextCausalLeg(
  bars: readonly Bar[],
  i: number,
  size: number,
  prevLeg: -1 | 0 | 1,
): { nextLeg: -1 | 0 | 1; pivotIdx: number } {
  const p = i - size;
  if (p < 0) return { nextLeg: prevLeg, pivotIdx: -1 };
  let maxAfter = -Infinity;
  let minAfter = Infinity;
  for (let k = p + 1; k <= i; k += 1) {
    if (bars[k].high > maxAfter) maxAfter = bars[k].high;
    if (bars[k].low < minAfter) minAfter = bars[k].low;
  }
  if (bars[p].high > maxAfter) return { nextLeg: 0, pivotIdx: p };
  if (bars[p].low < minAfter) return { nextLeg: 1, pivotIdx: p };
  return { nextLeg: prevLeg, pivotIdx: p };
}

function runStructureTier(
  bars: readonly Bar[],
  parsedHigh: Float64Array,
  parsedLow: Float64Array,
  size: number,
  tier: SmcTier,
): { breaks: SmcStructureBreak[]; activeObs: SmcOrderBlock[] } {
  let leg: -1 | 0 | 1 = -1;
  let trend: -1 | 0 | 1 = 0;
  let pivotHigh: ActivePivot | null = null;
  let pivotLow: ActivePivot | null = null;

  const breaks: SmcStructureBreak[] = [];
  let activeObs: InternalOrderBlock[] = [];

  for (let i = 0; i < bars.length; i += 1) {
    const bar = bars[i];

    // High/Low wick mitigation for blocks created on earlier bars.
    if (activeObs.length > 0) {
      activeObs = activeObs.filter((ob) => {
        if (ob.createdBreakIdx >= i) return true;
        return ob.direction === 'bull' ? bar.low >= ob.bottom : bar.high <= ob.top;
      });
    }

    if (i >= size) {
      const { nextLeg, pivotIdx } = nextCausalLeg(bars, i, size, leg);
      if (nextLeg !== leg && nextLeg !== -1) {
        if (nextLeg === 1) {
          pivotLow = {
            index: pivotIdx,
            time: bars[pivotIdx].time,
            level: bars[pivotIdx].low,
            crossed: false,
          };
        } else {
          pivotHigh = {
            index: pivotIdx,
            time: bars[pivotIdx].time,
            level: bars[pivotIdx].high,
            crossed: false,
          };
        }
        leg = nextLeg;
      }
    }

    if (pivotHigh && !pivotHigh.crossed && i > pivotHigh.index && bar.close > pivotHigh.level) {
      const kind: 'BOS' | 'CHoCH' = trend === -1 ? 'CHoCH' : 'BOS';
      trend = 1;
      pivotHigh.crossed = true;
      breaks.push({
        kind,
        tier,
        direction: 'bull',
        level: pivotHigh.level,
        startTime: pivotHigh.time,
        endTime: bar.time,
      });

      let minIdx = pivotHigh.index;
      for (let k = pivotHigh.index + 1; k <= i; k += 1) {
        if (parsedLow[k] < parsedLow[minIdx]) minIdx = k;
      }
      const top = Math.max(parsedHigh[minIdx], parsedLow[minIdx]);
      const bottom = Math.min(parsedHigh[minIdx], parsedLow[minIdx]);
      if (top > bottom && bottom > 0) {
        activeObs.push({
          tier,
          direction: 'bull',
          top,
          bottom,
          startTime: bars[minIdx].time,
          createdBreakIdx: i,
        });
        if (activeObs.length > 12) activeObs.shift();
      }
    }

    if (pivotLow && !pivotLow.crossed && i > pivotLow.index && bar.close < pivotLow.level) {
      const kind: 'BOS' | 'CHoCH' = trend === 1 ? 'CHoCH' : 'BOS';
      trend = -1;
      pivotLow.crossed = true;
      breaks.push({
        kind,
        tier,
        direction: 'bear',
        level: pivotLow.level,
        startTime: pivotLow.time,
        endTime: bar.time,
      });

      let maxIdx = pivotLow.index;
      for (let k = pivotLow.index + 1; k <= i; k += 1) {
        if (parsedHigh[k] > parsedHigh[maxIdx]) maxIdx = k;
      }
      const top = Math.max(parsedHigh[maxIdx], parsedLow[maxIdx]);
      const bottom = Math.min(parsedHigh[maxIdx], parsedLow[maxIdx]);
      if (top > bottom && bottom > 0) {
        activeObs.push({
          tier,
          direction: 'bear',
          top,
          bottom,
          startTime: bars[maxIdx].time,
          createdBreakIdx: i,
        });
        if (activeObs.length > 12) activeObs.shift();
      }
    }
  }

  return {
    breaks,
    activeObs: activeObs.map(({ tier: t, direction, top, bottom, startTime }) => ({
      tier: t,
      direction,
      top,
      bottom,
      startTime,
    })),
  };
}

function computeEqualLevels(bars: readonly Bar[], atr: Float64Array): SmcEqualLevel[] {
  let leg: -1 | 0 | 1 = -1;
  let prevHigh: { time: number; level: number } | null = null;
  let prevLow: { time: number; level: number } | null = null;
  let active: InternalEqualLevel[] = [];

  for (let i = 0; i < bars.length; i += 1) {
    const bar = bars[i];

    if (active.length > 0) {
      active = active.filter((eq) => {
        if (eq.confirmedIdx >= i) return true;
        return eq.kind === 'EQH' ? bar.high <= eq.sweepBound : bar.low >= eq.sweepBound;
      });
    }

    if (i < EQ_LENGTH) continue;
    const { nextLeg, pivotIdx } = nextCausalLeg(bars, i, EQ_LENGTH, leg);
    if (nextLeg === leg || nextLeg === -1) continue;
    leg = nextLeg;

    const pBar = bars[pivotIdx];
    const tol = EQ_ATR_THRESHOLD * atr[i];
    if (nextLeg === 0) {
      if (prevHigh && Math.abs(pBar.high - prevHigh.level) < tol) {
        active.push({
          kind: 'EQH',
          level: (pBar.high + prevHigh.level) / 2,
          startTime: prevHigh.time,
          endTime: pBar.time,
          sweepBound: Math.max(pBar.high, prevHigh.level),
          confirmedIdx: i,
        });
      }
      prevHigh = { time: pBar.time, level: pBar.high };
    } else {
      if (prevLow && Math.abs(pBar.low - prevLow.level) < tol) {
        active.push({
          kind: 'EQL',
          level: (pBar.low + prevLow.level) / 2,
          startTime: prevLow.time,
          endTime: pBar.time,
          sweepBound: Math.min(pBar.low, prevLow.level),
          confirmedIdx: i,
        });
      }
      prevLow = { time: pBar.time, level: pBar.low };
    }
  }

  return active
    .slice(-MAX_EQUAL_LEVELS)
    .map(({ kind, level, startTime, endTime }) => ({ kind, level, startTime, endTime }));
}

/**
 * Compute causal LuxAlgo-compatible Smart Money Concepts overlay from OHLC bars
 * (Swing 50 / Internal 5 / ATR High-Low mitigated Order Blocks / EQH-EQL 3 @ 0.1 ATR).
 * Safe for Hide-Future replay: depends strictly on bars[0..i] at step i.
 */
export function computeSmcOverlay(bars: readonly Bar[]): SmcOverlayData {
  if (bars.length < 15) {
    return { breaks: [], orderBlocks: [], equalLevels: [] };
  }

  const atr = computeCausalAtr(bars);
  const parsedHigh = new Float64Array(bars.length);
  const parsedLow = new Float64Array(bars.length);
  for (let i = 0; i < bars.length; i += 1) {
    const b = bars[i];
    const highVol = b.high - b.low >= 2 * atr[i];
    parsedHigh[i] = highVol ? b.low : b.high;
    parsedLow[i] = highVol ? b.high : b.low;
  }

  const swing = runStructureTier(bars, parsedHigh, parsedLow, SWING_LENGTH, 'swing');
  const internal = runStructureTier(bars, parsedHigh, parsedLow, INTERNAL_LENGTH, 'internal');

  const swingBreakKeys = new Set(swing.breaks.map((b) => `${b.startTime}:${b.level}`));
  const dedupInternalBreaks = internal.breaks.filter(
    (b) => !swingBreakKeys.has(`${b.startTime}:${b.level}`),
  );
  const breaks = [
    ...swing.breaks.slice(-MAX_SWING_BREAKS),
    ...dedupInternalBreaks.slice(-MAX_INTERNAL_BREAKS),
  ].sort((a, b) => a.endTime - b.endTime);

  const swingObKeys = new Set(swing.activeObs.map((ob) => `${ob.startTime}:${ob.top}:${ob.bottom}`));
  const chosenSwingObs = swing.activeObs.slice(-MAX_SWING_ORDER_BLOCKS);
  const remainingSlots = Math.max(0, MAX_ACTIVE_ORDER_BLOCKS - chosenSwingObs.length);
  const dedupInternalObs = internal.activeObs
    .filter((ob) => !swingObKeys.has(`${ob.startTime}:${ob.top}:${ob.bottom}`))
    .slice(-remainingSlots);

  const orderBlocks = [...chosenSwingObs, ...dedupInternalObs].sort(
    (a, b) => a.startTime - b.startTime,
  );

  const equalLevels = computeEqualLevels(bars, atr);
  return { breaks, orderBlocks, equalLevels };
}
