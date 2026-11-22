import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { ChevronDown, Columns2, GripHorizontal, Search, X } from 'lucide-react';
import KairosChart from '../chart/KairosChart';
import { syncChartGroup, type SyncChartHandle } from '../chart/syncCharts';
import { SegmentedControl, type SegmentedOption } from '../components/SegmentedControl';
import { useToast } from '../components/ToastViewport';
import {
  type AnomalyItem,
  type Bar,
  type ChartDrawingItem,
  type ChartDrawingPoint,
  type CycleMap,
  type CycleNode,
  type DrawingKind,
  type MetricsPoint,
  type Notice,
  type Timeframe,
  type WatchWorkspaceProps,
} from '../types/kairos';
import { useI18n } from '../i18n';
import { PHASE_LABEL, sameSymbol } from '../utils/doctrine';
import { TIMEFRAME_SECONDS, fmtDateTimeSec, fmtIsoUtc8, fmtPctPoints } from '../utils/format';

type WatchTranslate = ReturnType<typeof useI18n>['t'];

const HOTKEY_TFS: readonly Timeframe[] = ['1m', '5m', '15m', '1h', '4h'];

type QueueTab = 'all' | 'pulse' | 'major';

type QueueItem =
  | { kind: 'notice'; id: string; symbol: string; ts: number; notice: Notice }
  | { kind: 'anomaly'; id: string; symbol: string; ts: number; anomaly: AnomalyItem };

interface MoverChip {
  symbol: string;
  returnPct: number | null;
}

function anomalyTypeLabel(eventType: string, t: WatchTranslate): string {
  switch (eventType) {
    case 'price_velocity':
      return t('watch.anomaly.priceVelocity');
    case 'volume_spike':
      return t('watch.anomaly.volumeSpike');
    case 'open_interest_change':
      return t('watch.anomaly.oiChange');
    case 'funding_rate_anomaly':
      return t('watch.anomaly.fundingAnomaly');
    case 'resonance':
      return t('watch.anomaly.resonance');
    default:
      return eventType;
  }
}

interface ChartSliceState {
  bars: Bar[];
  metrics: MetricsPoint[];
  marketCycle: CycleMap | null;
  symbolCycle: CycleMap | null;
  loading: boolean;
  loadingMore?: boolean;
  error: string | null;
  errorKey?: 'watch.error.loadBars' | 'watch.error.loadCompareBars' | null;
}

const EMPTY_SLICE: ChartSliceState = {
  bars: [],
  metrics: [],
  marketCycle: null,
  symbolCycle: null,
  loading: false,
  loadingMore: false,
  error: null,
  errorKey: null,
};

function mergeBarsAscending(older: Bar[], current: Bar[]): Bar[] {
  const byTime = new Map<number, Bar>();
  for (const b of older) byTime.set(b.time, b);
  for (const b of current) byTime.set(b.time, b);
  return Array.from(byTime.values()).sort((a, b) => a.time - b.time);
}

function mergeLatestBars(
  prev: Bar[] | undefined,
  loaded: Bar[],
  tfSec: number,
): Bar[] {
  if (!prev || prev.length === 0 || loaded.length === 0) return loaded;
  const prevLast = prev[prev.length - 1];
  if (tfSec > 0 && loaded[0].time - prevLast.time > tfSec * 1.5) {
    return loaded;
  }
  return mergeBarsAscending(prev, loaded);
}

interface HistoryLoadGuard {
  key: string;
  inFlight: boolean;
  exhausted: boolean;
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
}

function cleanSymbol(sym: string, marketLabel: string): string {
  if (sym.trim().toUpperCase() === 'MARKET') return marketLabel;
  return sym.replace(/:USDT$/, '');
}

function shortTicker(sym: string): string {
  return sym.replace(/\/USDT(:USDT)?$/i, '').replace(/:USDT$/i, '');
}

function asFiniteNumber(v: unknown): number | null {
  if (typeof v === 'number' && Number.isFinite(v)) return v;
  if (typeof v === 'string' && v.trim() !== '') {
    const n = Number(v);
    if (Number.isFinite(n)) return n;
  }
  return null;
}

function isMarketAnomaly(a: AnomalyItem): boolean {
  return a.event_type.startsWith('market_');
}

function extractMoverChips(
  data: Record<string, unknown> | undefined,
  isDown: boolean,
  limit = 3,
): MoverChip[] {
  if (!data) return [];
  const detailKeys = isDown
    ? ['laggards_detail', 'laggard_detail']
    : ['leaders_detail', 'leader_detail'];
  const listKey = isDown ? 'laggards' : 'leaders';

  const out: MoverChip[] = [];
  const seen = new Set<string>();

  for (const key of detailKeys) {
    const arr = data[key];
    if (!Array.isArray(arr)) continue;
    for (const raw of arr) {
      if (!raw || typeof raw !== 'object') continue;
      const rec = raw as Record<string, unknown>;
      const sym = typeof rec.symbol === 'string' ? rec.symbol.trim() : '';
      if (!sym || sym.toUpperCase() === 'MARKET' || seen.has(sym)) continue;
      seen.add(sym);
      out.push({
        symbol: sym,
        returnPct: asFiniteNumber(rec.return_pct),
      });
      if (out.length >= limit) return out;
    }
  }

  const symArr = data[listKey];
  if (Array.isArray(symArr)) {
    for (const raw of symArr) {
      const sym = typeof raw === 'string' ? raw.trim() : '';
      if (!sym || sym.toUpperCase() === 'MARKET' || seen.has(sym)) continue;
      seen.add(sym);
      out.push({ symbol: sym, returnPct: null });
      if (out.length >= limit) return out;
    }
  }
  return out;
}

function marketAnomalyMeta(a: AnomalyItem, t: WatchTranslate): {
  title: string;
  tone: 'bull' | 'bear' | 'warn' | 'muted';
  med60: number | null;
  breadthRatio: number | null;
  chips: MoverChip[];
} {
  const data = a.data ?? {};
  const stateTo = typeof data.state_to === 'string' ? data.state_to : '';
  const dir = typeof data.direction === 'string' ? data.direction : '';
  const isDown = stateTo.includes('DOWN') || dir === 'down';
  const isUp = stateTo.includes('UP') || dir === 'up';

  let title = t('watch.pulse.defaultResonance');
  let tone: 'bull' | 'bear' | 'warn' | 'muted' = 'muted';
  switch (a.event_type) {
    case 'market_stress':
      title = isDown ? t('watch.pulse.stressDown') : t('watch.pulse.stressUp');
      tone = isDown ? 'bear' : 'warn';
      break;
    case 'market_trend':
      title = isDown ? t('watch.pulse.trendDown') : t('watch.pulse.trendUp');
      tone = isDown ? 'bear' : 'bull';
      break;
    case 'market_impulse':
      title = isDown ? t('watch.pulse.impulseDown') : t('watch.pulse.impulseUp');
      tone = isDown ? 'bear' : 'bull';
      break;
    case 'market_decay':
      title = t('watch.pulse.decay');
      tone = 'muted';
      break;
    case 'market_data_stale':
      title = t('watch.pulse.dataStale');
      tone = 'warn';
      break;
    case 'market_data_recovered':
      title = t('watch.pulse.dataRecovered');
      tone = 'muted';
      break;
    default:
      title = isUp
        ? t('watch.pulse.defaultUp')
        : isDown
          ? t('watch.pulse.defaultDown')
          : t('watch.pulse.defaultResonance');
      tone = isDown ? 'bear' : isUp ? 'bull' : 'muted';
      break;
  }

  const med60 =
    asFiniteNumber(data.median_return_60s_pct) ??
    asFiniteNumber(data.median_return_60s);
  const breadthRatio =
    asFiniteNumber(data.breadth) ??
    (isDown ? asFiniteNumber(data.down_breadth) : asFiniteNumber(data.up_breadth));
  const chips =
    a.event_type === 'market_data_stale' || a.event_type === 'market_data_recovered'
      ? []
      : extractMoverChips(data, isDown, 3);

  return { title, tone, med60, breadthRatio, chips };
}

function upsertBar(
  list: Bar[],
  incoming: Bar,
  tfSec: number,
  allowGap = false,
): Bar[] {
  if (list.length === 0) return [incoming];
  const last = list[list.length - 1];
  if (incoming.time === last.time) {
    if (
      incoming.open === last.open &&
      incoming.high === last.high &&
      incoming.low === last.low &&
      incoming.close === last.close &&
      incoming.volume === last.volume
    ) {
      return list;
    }
    const next = list.slice();
    next[next.length - 1] = incoming;
    return next;
  }
  if (incoming.time > last.time) {
    // Reject cross-gap live bars until REST backfill runs; if REST cannot
    // heal the gap (e.g. geo-blocked REST), allowGap lets live WS continue.
    if (!allowGap && tfSec > 0 && incoming.time - last.time > tfSec * 1.5) {
      return list;
    }
    const next = [...list, incoming];
    return next.length > 10000 ? next.slice(next.length - 10000) : next;
  }
  return list;
}

function pickNode(cycle: CycleMap | null | undefined, tfs: string[]): CycleNode | null {
  if (!cycle?.nodes) return null;
  for (const tf of tfs) {
    if (cycle.nodes[tf]) return cycle.nodes[tf];
  }
  return null;
}

function shortNodeHint(
  label: string,
  node: CycleNode | null,
  t: WatchTranslate,
): string | null {
  if (!node) return null;
  if (node.direction !== 'up' && node.direction !== 'down') {
    return t('watch.cycle.nodeNeutral', { tf: label });
  }
  const dir = node.direction === 'up' ? t('watch.dir.long') : t('watch.dir.short');
  return t('watch.cycle.nodeDirectional', {
    tf: label,
    dir,
    phase: PHASE_LABEL[node.phase],
  });
}

export function WatchWorkspace({
  client,
  bootstrap,
  activeSymbol,
  onSelectSymbol,
  splitMode,
  onToggleSplit,
  activeTf,
  onSelectTf,
}: WatchWorkspaceProps) {
  const { t, locale } = useI18n();
  const { pushToast } = useToast();

  // Queue drawer closed by default for 100% chart breathing room
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [queueTab, setQueueTab] = useState<QueueTab>('all');
  const [selectedQueueId, setSelectedQueueId] = useState<string>('');
  const queueToggleBtnRef = useRef<HTMLButtonElement | null>(null);
  const queueCloseBtnRef = useRef<HTMLButtonElement | null>(null);
  const queueDrawerRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (drawerOpen) {
      const timer = window.setTimeout(() => queueCloseBtnRef.current?.focus(), 10);
      return () => window.clearTimeout(timer);
    }
    if (
      queueDrawerRef.current &&
      document.activeElement &&
      queueDrawerRef.current.contains(document.activeElement)
    ) {
      queueToggleBtnRef.current?.focus();
    }
    return undefined;
  }, [drawerOpen]);

  // Resizable split ratio (primary pane fraction, persisted on drag end / key adjust)
  const splitContainerRef = useRef<HTMLDivElement | null>(null);
  const splitRatioRef = useRef<number>(0.55);
  const [splitRatio, setSplitRatio] = useState<number>(() => {
    try {
      const raw = Number(localStorage.getItem('kairos.desk.splitRatio'));
      if (Number.isFinite(raw) && raw >= 0.25 && raw <= 0.75) {
        splitRatioRef.current = raw;
        return raw;
      }
    } catch {
      // ignore storage errors
    }
    return 0.55;
  });
  splitRatioRef.current = splitRatio;
  const [draggingSplit, setDraggingSplit] = useState(false);
  const splitDragCleanupRef = useRef<(() => void) | null>(null);

  const persistSplitRatio = useCallback((ratio: number) => {
    try {
      localStorage.setItem('kairos.desk.splitRatio', String(ratio));
    } catch {
      // ignore storage errors
    }
  }, []);

  useEffect(() => {
    if (!splitMode) {
      splitDragCleanupRef.current?.();
      splitDragCleanupRef.current = null;
    }
  }, [splitMode]);

  useEffect(() => {
    return () => {
      splitDragCleanupRef.current?.();
      splitDragCleanupRef.current = null;
    };
  }, []);

  const handleSplitDividerPointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      if (e.button !== 0) return;
      const container = splitContainerRef.current;
      if (!container) return;
      e.preventDefault();

      splitDragCleanupRef.current?.();
      const handleEl = e.currentTarget;
      const pointerId = e.pointerId;
      try {
        handleEl.setPointerCapture(pointerId);
      } catch {
        // Ignore if pointer capture is unsupported.
      }
      setDraggingSplit(true);

      let rafId = 0;
      let pendingRatio: number | null = null;
      const rect = container.getBoundingClientRect();

      const onMove = (ev: PointerEvent) => {
        if (ev.pointerId !== pointerId || rect.height <= 0) return;
        pendingRatio = Math.min(0.75, Math.max(0.25, (ev.clientY - rect.top) / rect.height));
        if (!rafId) {
          rafId = requestAnimationFrame(() => {
            rafId = 0;
            if (pendingRatio != null) {
              setSplitRatio(pendingRatio);
            }
          });
        }
      };

      const finish = (ev?: PointerEvent) => {
        if (ev && ev.pointerId !== pointerId) return;
        if (rafId) {
          cancelAnimationFrame(rafId);
          rafId = 0;
        }
        if (pendingRatio != null) {
          setSplitRatio(pendingRatio);
          persistSplitRatio(pendingRatio);
          pendingRatio = null;
        } else {
          persistSplitRatio(splitRatioRef.current);
        }
        setDraggingSplit(false);
        try {
          if (handleEl.hasPointerCapture(pointerId)) {
            handleEl.releasePointerCapture(pointerId);
          }
        } catch {
          // Ignore release errors.
        }
        window.removeEventListener('pointermove', onMove);
        window.removeEventListener('pointerup', finish);
        window.removeEventListener('pointercancel', finish);
        window.removeEventListener('blur', onBlur);
        handleEl.removeEventListener('lostpointercapture', onLostCapture);
        splitDragCleanupRef.current = null;
      };

      const onBlur = () => finish();
      const onLostCapture = () => finish();

      splitDragCleanupRef.current = () => finish();
      window.addEventListener('pointermove', onMove);
      window.addEventListener('pointerup', finish);
      window.addEventListener('pointercancel', finish);
      window.addEventListener('blur', onBlur);
      handleEl.addEventListener('lostpointercapture', onLostCapture);
    },
    [persistSplitRatio],
  );

  const queueTabOptions = useMemo<ReadonlyArray<SegmentedOption<QueueTab>>>(
    () => [
      { value: 'all', label: t('watch.queue.tabAll'), activeColor: 'var(--accent-brand)' },
      { value: 'pulse', label: t('watch.queue.tabPulse'), activeColor: 'var(--accent-brand)' },
      { value: 'major', label: t('watch.queue.tabMajor'), activeColor: 'var(--accent-brand)' },
    ],
    [t],
  );

  // Bottom comparison chart independent symbol & timeframe
  const [compareSymbol, setCompareSymbol] = useState<string>(() =>
    sameSymbol(activeSymbol, 'ETH/USDT:USDT') ? 'BTC/USDT:USDT' : 'ETH/USDT:USDT',
  );
  const [compareTf, setCompareTf] = useState<Timeframe>(activeTf);

  // Chart data slices
  const [primarySlice, setPrimarySlice] = useState<ChartSliceState>(EMPTY_SLICE);
  const [compareSlice, setCompareSlice] = useState<ChartSliceState>(EMPTY_SLICE);
  const [drawings, setDrawings] = useState<ChartDrawingItem[]>([]);
  const [compareDrawings, setCompareDrawings] = useState<ChartDrawingItem[]>([]);

  // Per-pane infinite scroll guards & in-memory K-line slice cache
  const primaryHistoryRef = useRef<HistoryLoadGuard>({ key: '', inFlight: false, exhausted: false });
  const compareHistoryRef = useRef<HistoryLoadGuard>({ key: '', inFlight: false, exhausted: false });
  const primaryLastTimeRef = useRef<number>(0);
  primaryLastTimeRef.current = primarySlice.bars[primarySlice.bars.length - 1]?.time ?? 0;
  const compareLastTimeRef = useRef<number>(0);
  compareLastTimeRef.current = compareSlice.bars[compareSlice.bars.length - 1]?.time ?? 0;
  const refreshGuardRef = useRef<Map<string, { inFlight: boolean; lastAt: number }>>(new Map());
  const pendingGapBarRef = useRef<
    Map<
      string,
      {
        bar: Bar;
        setter: React.Dispatch<React.SetStateAction<ChartSliceState>>;
        isCurrent: () => boolean;
      }
    >
  >(new Map());
  const gapFallbackAnchorRef = useRef<Map<string, number>>(new Map());
  const paneRef = useRef({ activeSymbol, activeTf, splitMode, compareSymbol, compareTf });
  paneRef.current = { activeSymbol, activeTf, splitMode, compareSymbol, compareTf };
  const barCacheRef = useRef<
    Map<string, { bars: Bar[]; marketCycle: CycleMap | null; symbolCycle: CycleMap | null }>
  >(new Map());

  const writeBarCache = useCallback(
    (
      key: string,
      entry: { bars: Bar[]; marketCycle: CycleMap | null; symbolCycle: CycleMap | null },
    ) => {
      const cache = barCacheRef.current;
      if (cache.has(key)) {
        cache.delete(key);
      }
      cache.set(key, entry);
      while (cache.size > 16) {
        const oldest = cache.keys().next().value;
        if (oldest === undefined) break;
        cache.delete(oldest);
      }
    },
    [],
  );

  // Sync handles for Top/Bottom dual chart
  const [primaryHandle, setPrimaryHandle] = useState<SyncChartHandle | null>(null);
  const [compareHandle, setCompareHandle] = useState<SyncChartHandle | null>(null);

  // Queue items (All | Pulse | Major)
  const queueItems = useMemo<QueueItem[]>(() => {
    const out: QueueItem[] = [];
    if (queueTab === 'all' || queueTab === 'major') {
      for (const n of bootstrap.notices ?? []) {
        if (n.delivery_status === 'suppressed') continue;
        const tsSec = Math.floor((Date.parse(n.ts) || 0) / 1000);
        out.push({
          kind: 'notice',
          id: `n:${n.notice_id}`,
          symbol: n.symbol,
          ts: tsSec,
          notice: n,
        });
      }
    }
    for (const a of bootstrap.anomalies ?? []) {
      const marketEvt = isMarketAnomaly(a);
      if (queueTab === 'pulse' && !marketEvt) continue;
      if (queueTab === 'major' && marketEvt) continue;
      out.push({
        kind: 'anomaly',
        id: `a:${a.event_id ?? `${a.symbol}-${a.timestamp}-${a.event_type}`}`,
        symbol: a.symbol,
        ts: a.timestamp,
        anomaly: a,
      });
    }
    out.sort((a, b) => b.ts - a.ts);
    return out;
  }, [queueTab, bootstrap.notices, bootstrap.anomalies]);

  // Load primary chart bars whenever activeSymbol or activeTf changes (0ms instant from cache)
  useEffect(() => {
    let cancelled = false;
    const ac = new AbortController();
    const key = `${activeSymbol}|${activeTf}`;
    primaryHistoryRef.current = { key, inFlight: false, exhausted: false };

    const cached = barCacheRef.current.get(key);
    if (cached && cached.bars.length > 0) {
      setPrimarySlice({
        bars: cached.bars,
        metrics: [],
        marketCycle: cached.marketCycle,
        symbolCycle: cached.symbolCycle,
        loading: false,
        loadingMore: false,
        error: null,
        errorKey: null,
      });
    } else {
      setPrimarySlice({ ...EMPTY_SLICE, loading: true });
    }

    client
      .getMarketBars({ symbol: activeSymbol, tf: activeTf, limit: 800, signal: ac.signal })
      .then((res) => {
        if (cancelled) return;
        const loaded = res.bars ?? [];
        if (loaded.length < 2) {
          primaryHistoryRef.current.exhausted = true;
        }
        const prevCached = barCacheRef.current.get(key);
        const mergedBars = mergeLatestBars(
          prevCached?.bars,
          loaded,
          TIMEFRAME_SECONDS[activeTf] ?? 300,
        );
        const marketCycle = res.market_cycle ?? prevCached?.marketCycle ?? null;
        const symbolCycle = res.symbol_cycle ?? prevCached?.symbolCycle ?? null;
        writeBarCache(key, {
          bars: mergedBars,
          marketCycle,
          symbolCycle,
        });
        setPrimarySlice({
          bars: mergedBars,
          metrics: res.metrics ?? [],
          marketCycle,
          symbolCycle,
          loading: false,
          loadingMore: false,
          error: null,
          errorKey: null,
        });
      })
      .catch((err) => {
        if (cancelled) return;
        setPrimarySlice((prev) => ({
          ...prev,
          loading: false,
          error: err instanceof Error ? err.message : null,
          errorKey: err instanceof Error ? null : 'watch.error.loadBars',
        }));
      });

    return () => {
      cancelled = true;
      ac.abort();
    };
  }, [client, activeSymbol, activeTf, writeBarCache]);

  // Load primary symbol drawings only when activeSymbol changes (never on timeframe switch)
  useEffect(() => {
    let cancelled = false;
    const ac = new AbortController();
    client
      .getDrawings(activeSymbol, ac.signal)
      .then((list) => {
        if (!cancelled) setDrawings(list);
      })
      .catch(() => {
        if (!cancelled) setDrawings([]);
      });
    return () => {
      cancelled = true;
      ac.abort();
    };
  }, [client, activeSymbol]);

  // Load bottom comparison chart bars when splitMode is ON (0ms instant from cache)
  useEffect(() => {
    if (!splitMode) return;
    let cancelled = false;
    const ac = new AbortController();
    const key = `${compareSymbol}|${compareTf}`;
    compareHistoryRef.current = { key, inFlight: false, exhausted: false };

    const cached = barCacheRef.current.get(key);
    if (cached && cached.bars.length > 0) {
      setCompareSlice({
        bars: cached.bars,
        metrics: [],
        marketCycle: cached.marketCycle,
        symbolCycle: cached.symbolCycle,
        loading: false,
        loadingMore: false,
        error: null,
        errorKey: null,
      });
    } else {
      setCompareSlice({ ...EMPTY_SLICE, loading: true });
    }

    client
      .getMarketBars({ symbol: compareSymbol, tf: compareTf, limit: 800, signal: ac.signal })
      .then((res) => {
        if (cancelled) return;
        const loaded = res.bars ?? [];
        if (loaded.length < 2) {
          compareHistoryRef.current.exhausted = true;
        }
        const prevCached = barCacheRef.current.get(key);
        const mergedBars = mergeLatestBars(
          prevCached?.bars,
          loaded,
          TIMEFRAME_SECONDS[compareTf] ?? 300,
        );
        const marketCycle = res.market_cycle ?? prevCached?.marketCycle ?? null;
        const symbolCycle = res.symbol_cycle ?? prevCached?.symbolCycle ?? null;
        writeBarCache(key, {
          bars: mergedBars,
          marketCycle,
          symbolCycle,
        });
        setCompareSlice({
          bars: mergedBars,
          metrics: res.metrics ?? [],
          marketCycle,
          symbolCycle,
          loading: false,
          loadingMore: false,
          error: null,
          errorKey: null,
        });
      })
      .catch((err) => {
        if (cancelled) return;
        setCompareSlice((prev) => ({
          ...prev,
          loading: false,
          error: err instanceof Error ? err.message : null,
          errorKey: err instanceof Error ? null : 'watch.error.loadCompareBars',
        }));
      });

    return () => {
      cancelled = true;
      ac.abort();
    };
  }, [client, splitMode, compareSymbol, compareTf, writeBarCache]);

  // Load comparison symbol drawings only when compareSymbol changes
  useEffect(() => {
    if (!splitMode) return;
    let cancelled = false;
    const ac = new AbortController();
    client
      .getDrawings(compareSymbol, ac.signal)
      .then((list) => {
        if (!cancelled) setCompareDrawings(list);
      })
      .catch(() => {
        if (!cancelled) setCompareDrawings([]);
      });
    return () => {
      cancelled = true;
      ac.abort();
    };
  }, [client, splitMode, compareSymbol]);

  const loadOlderFor = useCallback(
    (
      sym: string,
      tf: Timeframe,
      oldestTimeSec: number,
      guardRef: React.MutableRefObject<HistoryLoadGuard>,
      setter: React.Dispatch<React.SetStateAction<ChartSliceState>>,
    ) => {
      const key = `${sym}|${tf}`;
      if (guardRef.current.key !== key) {
        guardRef.current = { key, inFlight: false, exhausted: false };
      }
      if (guardRef.current.inFlight || guardRef.current.exhausted || oldestTimeSec <= 0) {
        return;
      }
      guardRef.current.inFlight = true;
      setter((prev) => ({ ...prev, loadingMore: true }));

      client
        .getMarketBars({ symbol: sym, tf, limit: 800, beforeTs: oldestTimeSec })
        .then((res) => {
          if (guardRef.current.key !== key) return;
          const older = (res.bars ?? []).filter((b) => b.time < oldestTimeSec);
          if (older.length < 2) {
            guardRef.current.exhausted = true;
          }
          setter((prev) => {
            const nextBars = older.length > 0 ? mergeBarsAscending(older, prev.bars) : prev.bars;
            writeBarCache(key, {
              bars: nextBars,
              marketCycle: prev.marketCycle,
              symbolCycle: prev.symbolCycle,
            });
            return {
              ...prev,
              bars: nextBars,
              loadingMore: false,
            };
          });
        })
        .catch(() => {
          setter((prev) => ({ ...prev, loadingMore: false }));
        })
        .finally(() => {
          if (guardRef.current.key === key) {
            guardRef.current.inFlight = false;
          }
        });
    },
    [client, writeBarCache],
  );

  const loadMorePrimary = useCallback(
    (oldestTimeSec: number) => {
      loadOlderFor(activeSymbol, activeTf, oldestTimeSec, primaryHistoryRef, setPrimarySlice);
    },
    [loadOlderFor, activeSymbol, activeTf],
  );

  const loadMoreCompare = useCallback(
    (oldestTimeSec: number) => {
      if (!splitMode) return;
      loadOlderFor(compareSymbol, compareTf, oldestTimeSec, compareHistoryRef, setCompareSlice);
    },
    [loadOlderFor, splitMode, compareSymbol, compareTf],
  );

  const refreshLatestFor = useCallback(
    (
      sym: string,
      tf: Timeframe,
      setter: React.Dispatch<React.SetStateAction<ChartSliceState>>,
      isCurrentPane: () => boolean,
      force = false,
    ): 'started' | 'in_flight' | 'throttled' => {
      const key = `${sym}|${tf}`;
      const guard = refreshGuardRef.current.get(key);
      const now = Date.now();
      if (guard?.inFlight) return 'in_flight';
      if (!force && guard && now - guard.lastAt < 3000) return 'throttled';

      refreshGuardRef.current.set(key, { inFlight: true, lastAt: now });
      let refreshedLastTime = 0;
      client
        .getMarketBars({ symbol: sym, tf, limit: 800 })
        .then((res) => {
          const loaded = res.bars ?? [];
          if (loaded.length === 0) return;
          const tfSec = TIMEFRAME_SECONDS[tf] ?? 300;
          const prevCached = barCacheRef.current.get(key);
          const mergedBars = mergeLatestBars(prevCached?.bars, loaded, tfSec);
          refreshedLastTime = mergedBars[mergedBars.length - 1]?.time ?? 0;
          if (refreshedLastTime > 0 && gapFallbackAnchorRef.current.get(key) !== refreshedLastTime) {
            gapFallbackAnchorRef.current.delete(key);
          }
          const marketCycle = res.market_cycle ?? prevCached?.marketCycle ?? null;
          const symbolCycle = res.symbol_cycle ?? prevCached?.symbolCycle ?? null;
          writeBarCache(key, {
            bars: mergedBars,
            marketCycle,
            symbolCycle,
          });
          if (!isCurrentPane()) return;
          setter((prev) => ({
            ...prev,
            bars: mergedBars,
            metrics: res.metrics ?? prev.metrics,
            marketCycle,
            symbolCycle,
            loading: false,
            error: null,
            errorKey: null,
          }));
        })
        .catch(() => {
          // Keep existing bars if backfill fails; pending gap bar falls through below.
        })
        .finally(() => {
          const cur = refreshGuardRef.current.get(key);
          if (cur) {
            cur.inFlight = false;
          }
          const pending = pendingGapBarRef.current.get(key);
          if (!pending) return;
          pendingGapBarRef.current.delete(key);
          if (!pending.isCurrent()) return;
          const tfSec = TIMEFRAME_SECONDS[tf] ?? 300;
          const anchor =
            refreshedLastTime > 0
              ? refreshedLastTime
              : (barCacheRef.current.get(key)?.bars.slice(-1)[0]?.time ?? 0);
          const stillGapped = anchor > 0 && pending.bar.time - anchor > tfSec * 1.5;
          if (stillGapped) {
            gapFallbackAnchorRef.current.set(key, anchor);
          }
          pending.setter((prev) => {
            if (prev.loading && prev.bars.length === 0) return prev;
            const nextBars = upsertBar(prev.bars, pending.bar, tfSec, stillGapped);
            if (nextBars === prev.bars) return prev;
            writeBarCache(key, {
              bars: nextBars,
              marketCycle: prev.marketCycle,
              symbolCycle: prev.symbolCycle,
            });
            return { ...prev, bars: nextBars };
          });
        });
      return 'started';
    },
    [client, writeBarCache],
  );

  // Subscribe WebSocket to primary (+ optional compare) symbol & timeframe
  useEffect(() => {
    const subs: Array<{ symbol: string; tfs: Timeframe[] }> = [
      { symbol: activeSymbol, tfs: [activeTf] },
    ];
    if (splitMode && compareSymbol) {
      if (sameSymbol(compareSymbol, activeSymbol)) {
        if (compareTf !== activeTf) {
          subs[0].tfs.push(compareTf);
        }
      } else {
        subs.push({ symbol: compareSymbol, tfs: [compareTf] });
      }
    }
    client.subscribeMany(subs);

    const primaryTfSec = TIMEFRAME_SECONDS[activeTf] ?? 300;
    const compareTfSec = TIMEFRAME_SECONDS[compareTf] ?? 300;
    const isPrimaryCurrent = () =>
      sameSymbol(paneRef.current.activeSymbol, activeSymbol) &&
      paneRef.current.activeTf === activeTf;
    const isCompareCurrent = () =>
      paneRef.current.splitMode &&
      sameSymbol(paneRef.current.compareSymbol, compareSymbol) &&
      paneRef.current.compareTf === compareTf;

    const applyWsKline = (
      sym: string,
      tf: Timeframe,
      tfSec: number,
      lastTime: number,
      bar: Bar,
      setter: React.Dispatch<React.SetStateAction<ChartSliceState>>,
      isCurrent: () => boolean,
    ) => {
      const key = `${sym}|${tf}`;
      let allowGap = false;
      if (lastTime > 0 && bar.time - lastTime > tfSec * 1.5) {
        if (gapFallbackAnchorRef.current.get(key) === lastTime) {
          allowGap = true;
        } else {
          pendingGapBarRef.current.set(key, { bar, setter, isCurrent });
          const status = refreshLatestFor(sym, tf, setter, isCurrent);
          if (status !== 'throttled') {
            return;
          }
          pendingGapBarRef.current.delete(key);
          gapFallbackAnchorRef.current.set(key, lastTime);
          allowGap = true;
        }
      }
      setter((prev) => {
        if (prev.loading && prev.bars.length === 0) return prev;
        const nextBars = upsertBar(prev.bars, bar, tfSec, allowGap);
        if (nextBars === prev.bars) return prev;
        writeBarCache(key, {
          bars: nextBars,
          marketCycle: prev.marketCycle,
          symbolCycle: prev.symbolCycle,
        });
        return { ...prev, bars: nextBars };
      });
    };

    const unsub = client.onWsEvent((evt) => {
      if (evt.type !== 'kline') return;
      if (sameSymbol(evt.symbol, activeSymbol) && evt.tf === activeTf) {
        applyWsKline(
          activeSymbol,
          activeTf,
          primaryTfSec,
          primaryLastTimeRef.current,
          evt.bar,
          setPrimarySlice,
          isPrimaryCurrent,
        );
      }
      if (splitMode && sameSymbol(evt.symbol, compareSymbol) && evt.tf === compareTf) {
        applyWsKline(
          compareSymbol,
          compareTf,
          compareTfSec,
          compareLastTimeRef.current,
          evt.bar,
          setCompareSlice,
          isCompareCurrent,
        );
      }
    });

    let wasDisconnected = !client.isWsConnected();
    const unsubConn = client.onConnectionChange((connected) => {
      if (!connected) {
        wasDisconnected = true;
        return;
      }
      if (!wasDisconnected) return;
      wasDisconnected = false;
      gapFallbackAnchorRef.current.delete(`${activeSymbol}|${activeTf}`);
      refreshLatestFor(activeSymbol, activeTf, setPrimarySlice, isPrimaryCurrent, true);
      if (splitMode && compareSymbol) {
        gapFallbackAnchorRef.current.delete(`${compareSymbol}|${compareTf}`);
        refreshLatestFor(compareSymbol, compareTf, setCompareSlice, isCompareCurrent, true);
      }
    });

    const onVisibilityChange = () => {
      if (document.visibilityState !== 'visible') return;
      const nowSec = Math.floor(Date.now() / 1000);
      if (primaryLastTimeRef.current > 0 && nowSec - primaryLastTimeRef.current >= primaryTfSec) {
        gapFallbackAnchorRef.current.delete(`${activeSymbol}|${activeTf}`);
        refreshLatestFor(activeSymbol, activeTf, setPrimarySlice, isPrimaryCurrent);
      }
      if (
        splitMode &&
        compareSymbol &&
        compareLastTimeRef.current > 0 &&
        nowSec - compareLastTimeRef.current >= compareTfSec
      ) {
        gapFallbackAnchorRef.current.delete(`${compareSymbol}|${compareTf}`);
        refreshLatestFor(compareSymbol, compareTf, setCompareSlice, isCompareCurrent);
      }
    };
    document.addEventListener('visibilitychange', onVisibilityChange);

    return () => {
      unsub();
      unsubConn();
      document.removeEventListener('visibilitychange', onVisibilityChange);
    };
  }, [
    client,
    activeSymbol,
    activeTf,
    splitMode,
    compareSymbol,
    compareTf,
    writeBarCache,
    refreshLatestFor,
  ]);

  // Refresh active panes immediately when proxy_url setting changes
  const prevProxyUrlRef = useRef<string | undefined>(bootstrap.status.proxy_url);
  useEffect(() => {
    const nextProxy = bootstrap.status.proxy_url;
    if (prevProxyUrlRef.current === nextProxy) return;
    prevProxyUrlRef.current = nextProxy;
    gapFallbackAnchorRef.current.clear();
    const p = paneRef.current;
    refreshLatestFor(
      p.activeSymbol,
      p.activeTf,
      setPrimarySlice,
      () =>
        sameSymbol(paneRef.current.activeSymbol, p.activeSymbol) &&
        paneRef.current.activeTf === p.activeTf,
      true,
    );
    if (p.splitMode && p.compareSymbol) {
      refreshLatestFor(
        p.compareSymbol,
        p.compareTf,
        setCompareSlice,
        () =>
          paneRef.current.splitMode &&
          sameSymbol(paneRef.current.compareSymbol, p.compareSymbol) &&
          paneRef.current.compareTf === p.compareTf,
        true,
      );
    }
  }, [bootstrap.status.proxy_url, refreshLatestFor]);

  // Synchronize Top/Bottom dual charts (re-attaches cleanly once both have bars)
  useEffect(() => {
    if (!splitMode || !primaryHandle || !compareHandle) return;
    if (primarySlice.bars.length === 0 || compareSlice.bars.length === 0) return;
    return syncChartGroup([primaryHandle, compareHandle]);
  }, [
    splitMode,
    primaryHandle,
    compareHandle,
    primarySlice.bars.length > 0,
    compareSlice.bars.length > 0,
  ]);

  // Drawing persistence handlers (0ms optimistic updates)
  const handleUserDrawing = useCallback(
    async (kind: DrawingKind, points: ChartDrawingPoint[]) => {
      const nowSec = Math.floor(Date.now() / 1000);
      const tempId = -Date.now() - Math.floor(Math.random() * 1000);
      const optimistic: ChartDrawingItem = {
        id: tempId,
        symbol: activeSymbol,
        kind,
        points,
        created_at: nowSec,
        updated_at: nowSec,
      };
      setDrawings((prev) => [...prev, optimistic]);
      try {
        const created = await client.createDrawing({
          symbol: activeSymbol,
          kind,
          points,
        });
        setDrawings((prev) =>
          prev.map((d) => (d.id === tempId ? { ...created, points: d.points } : d)),
        );
      } catch (err) {
        console.warn('保存画线失败', err);
      }
    },
    [client, activeSymbol],
  );

  const handleDeleteDrawing = useCallback(
    async (id: number) => {
      setDrawings((prev) => prev.filter((d) => d.id !== id));
      if (id <= 0) return;
      try {
        await client.deleteDrawing(id);
      } catch (err) {
        console.warn('删除画线失败', err);
      }
    },
    [client],
  );

  const handleUpdateDrawing = useCallback(
    async (id: number, points: ChartDrawingPoint[]) => {
      setDrawings((prev) => prev.map((d) => (d.id === id ? { ...d, points } : d)));
      if (id <= 0) return;
      try {
        const updated = await client.updateDrawing(id, { points });
        setDrawings((prev) => prev.map((d) => (d.id === id ? updated : d)));
      } catch (err) {
        console.warn('更新画线失败', err);
      }
    },
    [client],
  );

  const handleEraseDrawings = useCallback(async () => {
    setDrawings([]);
    try {
      await client.deleteDrawingsForSymbol(activeSymbol);
      pushToast({ tone: 'info', title: t('toast.drawingsCleared'), durationMs: 2200 });
    } catch (err) {
      console.warn('清空画线失败', err);
    }
  }, [client, activeSymbol, pushToast, t]);

  const handleCompareDrawing = useCallback(
    async (kind: DrawingKind, points: ChartDrawingPoint[]) => {
      const nowSec = Math.floor(Date.now() / 1000);
      const tempId = -Date.now() - Math.floor(Math.random() * 1000);
      const optimistic: ChartDrawingItem = {
        id: tempId,
        symbol: compareSymbol,
        kind,
        points,
        created_at: nowSec,
        updated_at: nowSec,
      };
      setCompareDrawings((prev) => [...prev, optimistic]);
      try {
        const created = await client.createDrawing({
          symbol: compareSymbol,
          kind,
          points,
        });
        setCompareDrawings((prev) =>
          prev.map((d) => (d.id === tempId ? { ...created, points: d.points } : d)),
        );
      } catch (err) {
        console.warn('保存画线失败', err);
      }
    },
    [client, compareSymbol],
  );

  const handleDeleteCompareDrawing = useCallback(
    async (id: number) => {
      setCompareDrawings((prev) => prev.filter((d) => d.id !== id));
      if (id <= 0) return;
      try {
        await client.deleteDrawing(id);
      } catch (err) {
        console.warn('删除画线失败', err);
      }
    },
    [client],
  );

  const handleUpdateCompareDrawing = useCallback(
    async (id: number, points: ChartDrawingPoint[]) => {
      setCompareDrawings((prev) => prev.map((d) => (d.id === id ? { ...d, points } : d)));
      if (id <= 0) return;
      try {
        const updated = await client.updateDrawing(id, { points });
        setCompareDrawings((prev) => prev.map((d) => (d.id === id ? updated : d)));
      } catch (err) {
        console.warn('更新画线失败', err);
      }
    },
    [client],
  );

  const handleEraseCompareDrawings = useCallback(async () => {
    setCompareDrawings([]);
    try {
      await client.deleteDrawingsForSymbol(compareSymbol);
      pushToast({ tone: 'info', title: t('toast.drawingsCleared'), durationMs: 2200 });
    } catch (err) {
      console.warn('清空画线失败', err);
    }
  }, [client, compareSymbol, pushToast, t]);

  const selectQueueItem = useCallback(
    (item: QueueItem) => {
      setSelectedQueueId(item.id);
      if (
        item.symbol &&
        item.symbol.toUpperCase() !== 'MARKET' &&
        !sameSymbol(item.symbol, activeSymbol)
      ) {
        onSelectSymbol(item.symbol);
      }
    },
    [activeSymbol, onSelectSymbol],
  );

  // Keep latest handlers in ref for keyboard shortcuts
  const hotkeyRef = useRef({
    queueItems,
    selectedQueueId,
    selectQueueItem,
    onSelectTf,
    onToggleSplit,
    splitMode,
    drawerOpen,
  });
  hotkeyRef.current = {
    queueItems,
    selectedQueueId,
    selectQueueItem,
    onSelectTf,
    onToggleSplit,
    splitMode,
    drawerOpen,
  };

  // Keyboard shortcuts (1-5 TFs, s split, j/k nav, Esc close queue drawer)
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === 'Escape' && hotkeyRef.current.drawerOpen) {
        e.preventDefault();
        e.stopImmediatePropagation();
        setDrawerOpen(false);
        return;
      }
      if (isEditableTarget(e.target) || e.shiftKey) return;

      const h = hotkeyRef.current;

      if (e.key >= '1' && e.key <= '5') {
        const tf = HOTKEY_TFS[Number(e.key) - 1];
        if (tf) {
          e.preventDefault();
          h.onSelectTf(tf);
        }
        return;
      }

      const lower = e.key.toLowerCase();
      if (lower === 's') {
        e.preventDefault();
        h.onToggleSplit();
        return;
      }
      if ((lower === 'j' || lower === 'k') && h.drawerOpen && h.queueItems.length > 0) {
        e.preventDefault();
        const idx = h.queueItems.findIndex((q) => q.id === h.selectedQueueId);
        const nextIdx =
          lower === 'j'
            ? idx < 0
              ? 0
              : Math.min(h.queueItems.length - 1, idx + 1)
            : idx < 0
              ? 0
              : Math.max(0, idx - 1);
        h.selectQueueItem(h.queueItems[nextIdx]);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, []);

  // Filtered overlays for activeSymbol (only drawn when 异动 drawer is open)
  const symbolAnomalies = useMemo(
    () => bootstrap.anomalies.filter((a) => sameSymbol(a.symbol, activeSymbol)),
    [bootstrap.anomalies, activeSymbol],
  );
  const visibleSymbolNotices = useMemo(
    () =>
      bootstrap.notices.filter(
        (n) =>
          n.delivery_status !== 'suppressed' && sameSymbol(n.symbol, activeSymbol),
      ),
    [bootstrap.notices, activeSymbol],
  );

  // Quiet inline CycleMap hint text
  const symbolCycle =
    primarySlice.symbolCycle ??
    bootstrap.status.market_cycle ??
    null;
  const cycleHintText = useMemo(() => {
    const contextHint = shortNodeHint('4h', pickNode(symbolCycle, ['4h', '1d']), t);
    const setupHint = shortNodeHint('15m', pickNode(symbolCycle, ['15m', '1h']), t);
    return [contextHint, setupHint].filter(Boolean).join(' · ');
  }, [symbolCycle, t, locale]);

  const marketAllLabel = t('watch.marketAll');
  const primaryError =
    primarySlice.error ?? (primarySlice.errorKey ? t(primarySlice.errorKey) : null);
  const compareError =
    compareSlice.error ?? (compareSlice.errorKey ? t(compareSlice.errorKey) : null);

  const cryptoSymbols = useMemo(() => {
    const raw =
      bootstrap.status.available_symbols && bootstrap.status.available_symbols.length > 0
        ? bootstrap.status.available_symbols
        : bootstrap.status.symbols ?? [];
    return Array.from(
      new Set(raw.filter((s) => Boolean(s) && s.toUpperCase() !== 'MARKET')),
    );
  }, [bootstrap.status.available_symbols, bootstrap.status.symbols]);

  // Inline KISS controls inside KairosChart toolbar
  const chartControls = (
    <div className="flex shrink-0 items-center gap-1.5">
      {cycleHintText ? (
        <span
          className="hidden px-1 text-[11px] md:inline"
          style={{ color: 'var(--text-muted)' }}
          title={t('watch.cycle.hintTitle')}
        >
          {cycleHintText}
        </span>
      ) : null}

      <button
        type="button"
        onClick={onToggleSplit}
        title={t('watch.split.tooltip')}
        className="kairos-btn flex items-center gap-1 rounded-[5px] border px-1.5 py-0.5 text-[11px] font-medium"
        style={{
          backgroundColor: splitMode ? 'var(--accent-brand-soft)' : 'transparent',
          borderColor: splitMode ? 'var(--accent-brand)' : 'var(--border-hairline)',
          color: splitMode ? 'var(--accent-brand)' : 'var(--text-secondary)',
        }}
      >
        <Columns2 className="h-3 w-3" />
        <span>{t('watch.split.label')}</span>
      </button>

      <button
        ref={queueToggleBtnRef}
        type="button"
        onClick={() => setDrawerOpen((v) => !v)}
        title={t('watch.queue.toggleTooltip')}
        className="kairos-btn flex items-center gap-1.5 rounded-[5px] border px-1.5 py-0.5 text-[11px] font-medium"
        style={{
          backgroundColor: drawerOpen ? 'var(--accent-brand-soft)' : 'transparent',
          borderColor: drawerOpen ? 'var(--accent-brand)' : 'var(--border-hairline)',
          color: drawerOpen ? 'var(--accent-brand-text)' : 'var(--text-secondary)',
        }}
      >
        <span>{t('watch.queue.toggleLabel')}</span>
        {queueItems.length > 0 ? (
          <span
            className="inline-flex h-1.5 w-1.5 rounded-full"
            style={{ backgroundColor: 'var(--accent-brand)' }}
          />
        ) : null}
      </button>
    </div>
  );

  return (
    <div
      className="relative flex h-full min-h-0 w-full overflow-hidden"
      style={{ backgroundColor: 'var(--bg-canvas)' }}
    >
      {/* Left Slide-Over Queue Drawer (GPU slide transition, inert when closed) */}
      <div
        data-open={drawerOpen}
        className="kairos-drawer-scrim kairos-glass-backdrop absolute inset-0 z-10 sm:hidden"
        onClick={() => setDrawerOpen(false)}
        aria-hidden="true"
      />
      <aside
        ref={queueDrawerRef}
        data-open={drawerOpen}
        aria-hidden={!drawerOpen}
        inert={!drawerOpen ? true : undefined}
        className="kairos-drawer-left kairos-contain-panel absolute top-0 bottom-0 left-0 z-20 flex w-[min(86vw,280px)] flex-col border-r sm:w-[272px]"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-strong)',
          boxShadow: drawerOpen ? 'var(--shadow-floating)' : 'none',
        }}
      >
        <div
          className="flex items-center justify-between gap-2 border-b px-2.5 py-2"
          style={{ borderColor: 'var(--border-hairline)' }}
        >
          <SegmentedControl<QueueTab>
            options={queueTabOptions}
            value={queueTab}
            onChange={setQueueTab}
            size="xs"
            fullWidth
          />
          <button
            ref={queueCloseBtnRef}
            type="button"
            onClick={() => setDrawerOpen(false)}
            className="kairos-btn shrink-0 rounded-[5px] border border-transparent p-1"
            style={{ color: 'var(--text-muted)' }}
            aria-label={t('watch.queue.closeAria')}
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>

        <div className="min-h-0 flex-1 space-y-1.5 overflow-y-auto p-2">
          {!drawerOpen ? null : queueItems.length === 0 ? (
            <div
              className="px-3 py-10 text-center text-xs"
              style={{ color: 'var(--text-muted)' }}
            >
              {t('watch.queue.empty')}
            </div>
          ) : (
            queueItems.map((item) => {
              const isSelected = item.id === selectedQueueId;
              const marketMeta =
                item.kind === 'anomaly' && isMarketAnomaly(item.anomaly)
                  ? marketAnomalyMeta(item.anomaly, t)
                  : null;
              const stripeColor =
                item.kind === 'notice'
                  ? 'var(--accent-brand)'
                  : marketMeta
                    ? marketMeta.tone === 'bear'
                      ? 'var(--pnl-neg)'
                      : marketMeta.tone === 'bull'
                        ? 'var(--pnl-pos)'
                        : marketMeta.tone === 'warn'
                          ? 'var(--state-warn)'
                          : 'var(--border-strong)'
                    : item.anomaly.severity === 'HIGH'
                      ? 'var(--pnl-neg)'
                      : 'var(--state-warn)';

              return (
                <div
                  key={item.id}
                  role="button"
                  tabIndex={0}
                  onClick={() => selectQueueItem(item)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault();
                      selectQueueItem(item);
                    }
                  }}
                  className="kairos-list-item kairos-row relative w-full cursor-pointer overflow-hidden rounded-[6px] border py-2 pr-2.5 pl-3 text-left"
                  style={{
                    backgroundColor: isSelected
                      ? 'var(--bg-elevated)'
                      : 'var(--bg-canvas)',
                    borderColor: isSelected
                      ? 'var(--accent-brand)'
                      : 'var(--border-hairline)',
                  }}
                >
                  <span
                    aria-hidden="true"
                    className="absolute top-0 bottom-0 left-0 w-[2.5px]"
                    style={{ backgroundColor: stripeColor }}
                  />
                  {item.kind === 'notice' ? (
                    <>
                      <div className="flex items-center justify-between gap-1">
                        <span
                          className="font-mono text-xs font-medium tabular-nums"
                          style={{ color: 'var(--text-primary)' }}
                        >
                          {cleanSymbol(item.notice.symbol, marketAllLabel)}
                        </span>
                        <span
                          className="rounded px-1 py-0.5 text-[10px] font-medium"
                          style={{
                            backgroundColor: 'var(--accent-brand-soft)',
                            color: 'var(--accent-brand-text)',
                          }}
                        >
                          {t('watch.queue.noticeBadge')}
                        </span>
                      </div>
                      <div
                        className="mt-1 line-clamp-2 text-[11px]"
                        style={{ color: 'var(--text-secondary)' }}
                      >
                        {item.notice.trigger_facts.fact_summary ||
                          anomalyTypeLabel(item.notice.trigger_facts.event_type, t)}
                      </div>
                      <div
                        className="mt-1 font-mono text-[10px] tabular-nums"
                        style={{ color: 'var(--text-muted)' }}
                      >
                        {fmtIsoUtc8(item.notice.ts)}
                      </div>
                    </>
                  ) : marketMeta ? (
                    (() => {
                      const titleColor =
                        marketMeta.tone === 'bear'
                          ? 'var(--pnl-neg)'
                          : marketMeta.tone === 'bull'
                            ? 'var(--pnl-pos)'
                            : marketMeta.tone === 'warn'
                              ? 'var(--state-warn)'
                              : 'var(--text-primary)';
                      return (
                        <>
                          <div className="flex items-center justify-between gap-1">
                            <span className="text-xs font-medium" style={{ color: titleColor }}>
                              {marketMeta.title}
                            </span>
                            <span
                              className="font-mono text-[10px] tabular-nums"
                              style={{ color: 'var(--text-muted)' }}
                            >
                              {fmtDateTimeSec(item.anomaly.timestamp)}
                            </span>
                          </div>
                          {item.anomaly.event_type === 'market_data_stale' ||
                          item.anomaly.event_type === 'market_data_recovered' ? (
                            <div
                              className="mt-1 flex items-center justify-between font-mono text-[11px] tabular-nums"
                              style={{ color: 'var(--text-secondary)' }}
                            >
                              <span>
                                {t('watch.queue.coverage')}{' '}
                                {(() => {
                                  const cov = asFiniteNumber(item.anomaly.data?.coverage);
                                  return cov != null ? `${Math.round(cov * 100)}%` : '—';
                                })()}
                              </span>
                              <span>
                                {t('watch.queue.validSymbols')}{' '}
                                {asFiniteNumber(item.anomaly.data?.valid_symbols) ?? '—'}
                              </span>
                            </div>
                          ) : (
                            <div
                              className="mt-1 flex items-center justify-between font-mono text-[11px] tabular-nums"
                              style={{ color: 'var(--text-secondary)' }}
                            >
                              <span>
                                {t('watch.queue.median60s')}{' '}
                                <span
                                  style={{
                                    color:
                                      marketMeta.med60 == null
                                        ? 'var(--text-muted)'
                                        : marketMeta.med60 >= 0
                                          ? 'var(--pnl-pos)'
                                          : 'var(--pnl-neg)',
                                  }}
                                >
                                  {marketMeta.med60 != null ? fmtPctPoints(marketMeta.med60, 2, true) : '—'}
                                </span>
                              </span>
                              <span>
                                {t('watch.queue.breadth')}{' '}
                                {marketMeta.breadthRatio != null
                                  ? `${Math.round(marketMeta.breadthRatio * 100)}%`
                                  : '—'}
                              </span>
                            </div>
                          )}
                          {marketMeta.chips.length > 0 ? (
                            <div className="mt-1.5 flex flex-wrap gap-1">
                              {marketMeta.chips.map((chip) => (
                                <button
                                  key={chip.symbol}
                                  type="button"
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    setSelectedQueueId(item.id);
                                    onSelectSymbol(chip.symbol);
                                  }}
                                  className="kairos-btn tabular-nums flex items-center gap-1 rounded-[5px] border px-1.5 py-0.5 text-[10px]"
                                  style={{
                                    backgroundColor: 'var(--bg-surface)',
                                    borderColor: 'var(--border-hairline)',
                                    color: 'var(--text-primary)',
                                  }}
                                  title={t('watch.queue.switchSymbolTitle', {
                                    symbol: cleanSymbol(chip.symbol, marketAllLabel),
                                  })}
                                >
                                  <span className="font-medium">{shortTicker(chip.symbol)}</span>
                                  {chip.returnPct != null ? (
                                    <span
                                      style={{
                                        color:
                                          chip.returnPct >= 0
                                            ? 'var(--pnl-pos)'
                                            : 'var(--pnl-neg)',
                                      }}
                                    >
                                      {fmtPctPoints(chip.returnPct, 2, true)}
                                    </span>
                                  ) : null}
                                </button>
                              ))}
                            </div>
                          ) : null}
                        </>
                      );
                    })()
                  ) : (
                    (() => {
                      const changePct = asFiniteNumber(item.anomaly.data?.change_pct);
                      const winSec = asFiniteNumber(item.anomaly.data?.window_seconds);
                      const label = anomalyTypeLabel(item.anomaly.event_type, t);
                      return (
                        <>
                          <div className="flex items-center justify-between gap-1">
                            <span
                              className="font-mono text-xs font-medium tabular-nums"
                              style={{ color: 'var(--text-primary)' }}
                            >
                              {cleanSymbol(item.anomaly.symbol, marketAllLabel)}
                            </span>
                            {changePct != null ? (
                              <span
                                className="font-mono text-[11px] font-medium tabular-nums"
                                style={{
                                  color: changePct >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)',
                                }}
                              >
                                {fmtPctPoints(changePct, 2, true)}
                              </span>
                            ) : (
                              <span
                                className="font-mono text-[10px]"
                                style={{
                                  color:
                                    item.anomaly.severity === 'HIGH'
                                      ? 'var(--pnl-neg)'
                                      : 'var(--state-warn)',
                                }}
                              >
                                {item.anomaly.severity}
                              </span>
                            )}
                          </div>
                          <div
                            className="mt-0.5 flex items-center justify-between font-mono text-[10px] tabular-nums"
                            style={{ color: 'var(--text-muted)' }}
                          >
                            <span>{winSec != null ? `${label} · ${Math.round(winSec)}s` : label}</span>
                            <span>{fmtDateTimeSec(item.anomaly.timestamp)}</span>
                          </div>
                        </>
                      );
                    })()
                  )}
                </div>
              );
            })
          )}
        </div>
      </aside>

      {/* Main 100% Full-Bleed Chart Stage */}
      <section className="flex min-w-0 flex-1 flex-col">
        {splitMode ? (
          <div
            ref={splitContainerRef}
            className={`flex min-h-0 flex-1 flex-col ${draggingSplit ? 'select-none' : ''}`}
          >
            <div
              className="flex min-h-0 flex-col overflow-hidden"
              style={{ flex: `${splitRatio} 1 0%` }}
            >
              <KairosChart
                chartId="watch-primary"
                symbol={activeSymbol}
                timeframe={activeTf}
                availableTimeframes={HOTKEY_TFS}
                onTimeframeChange={onSelectTf}
                headerBadge={t('watch.split.primaryBadge')}
                statusSlot={chartControls}
                bars={primarySlice.bars}
                metrics={primarySlice.metrics}
                anomalies={drawerOpen ? symbolAnomalies : []}
                notices={drawerOpen ? visibleSymbolNotices : []}
                drawings={drawings}
                loading={primarySlice.loading}
                loadingMore={Boolean(primarySlice.loadingMore)}
                error={primaryError}
                followLatest
                onLoadMoreHistory={loadMorePrimary}
                onUserDrawing={handleUserDrawing}
                onUpdateDrawing={handleUpdateDrawing}
                onDeleteDrawing={handleDeleteDrawing}
                onEraseDrawings={handleEraseDrawings}
                onChartReady={setPrimaryHandle}
              />
            </div>

            {/* Interactive Split Resize Handle */}
            <div
              role="separator"
              tabIndex={0}
              aria-orientation="horizontal"
              aria-label={t('watch.split.resizeAria')}
              aria-valuemin={25}
              aria-valuemax={75}
              aria-valuenow={Math.round(splitRatio * 100)}
              title={t('watch.split.resizeTitle')}
              onPointerDown={handleSplitDividerPointerDown}
              onDoubleClick={() => {
                setSplitRatio(0.5);
                persistSplitRatio(0.5);
              }}
              onKeyDown={(e) => {
                if (e.key === 'ArrowUp') {
                  e.preventDefault();
                  const next = Math.max(0.25, Number((splitRatioRef.current - 0.05).toFixed(2)));
                  setSplitRatio(next);
                  persistSplitRatio(next);
                } else if (e.key === 'ArrowDown') {
                  e.preventDefault();
                  const next = Math.min(0.75, Number((splitRatioRef.current + 0.05).toFixed(2)));
                  setSplitRatio(next);
                  persistSplitRatio(next);
                } else if (e.key === 'Enter' || e.key === ' ') {
                  e.preventDefault();
                  setSplitRatio(0.5);
                  persistSplitRatio(0.5);
                }
              }}
              className="group relative flex h-[7px] shrink-0 cursor-row-resize items-center justify-center border-y transition-colors"
              style={{
                backgroundColor: draggingSplit
                  ? 'var(--accent-brand-soft)'
                  : 'var(--bg-surface)',
                borderColor: draggingSplit
                  ? 'var(--accent-brand)'
                  : 'var(--border-hairline)',
              }}
            >
              <GripHorizontal
                className="h-3 w-3 transition-colors group-hover:text-[var(--accent-brand)]"
                style={{
                  color: draggingSplit ? 'var(--accent-brand)' : 'var(--text-muted)',
                }}
              />
            </div>

            <div
              className="flex min-h-0 flex-col overflow-hidden"
              style={{ flex: `${1 - splitRatio} 1 0%` }}
            >
              <KairosChart
                chartId="watch-compare"
                symbol={compareSymbol}
                timeframe={compareTf}
                availableTimeframes={HOTKEY_TFS}
                onTimeframeChange={setCompareTf}
                symbolSlot={
                  <CompareSymbolPicker
                    symbol={compareSymbol}
                    symbols={cryptoSymbols}
                    onSelect={(nextSym) => {
                      if (sameSymbol(nextSym, compareSymbol)) return;
                      setCompareSymbol(nextSym);
                    }}
                  />
                }
                headerBadge={t('watch.split.compareBadge')}
                onClosePane={onToggleSplit}
                bars={compareSlice.bars}
                metrics={compareSlice.metrics}
                drawings={compareDrawings}
                loading={compareSlice.loading}
                loadingMore={Boolean(compareSlice.loadingMore)}
                error={compareError}
                followLatest
                onLoadMoreHistory={loadMoreCompare}
                onUserDrawing={handleCompareDrawing}
                onUpdateDrawing={handleUpdateCompareDrawing}
                onDeleteDrawing={handleDeleteCompareDrawing}
                onEraseDrawings={handleEraseCompareDrawings}
                onChartReady={setCompareHandle}
              />
            </div>
          </div>
        ) : (
          <KairosChart
            chartId="watch-single"
            symbol={activeSymbol}
            timeframe={activeTf}
            availableTimeframes={HOTKEY_TFS}
            onTimeframeChange={onSelectTf}
            statusSlot={chartControls}
            bars={primarySlice.bars}
            metrics={primarySlice.metrics}
            anomalies={drawerOpen ? symbolAnomalies : []}
            notices={drawerOpen ? visibleSymbolNotices : []}
            drawings={drawings}
            loading={primarySlice.loading}
            loadingMore={Boolean(primarySlice.loadingMore)}
            error={primaryError}
            followLatest
            onLoadMoreHistory={loadMorePrimary}
            onUserDrawing={handleUserDrawing}
            onUpdateDrawing={handleUpdateDrawing}
            onDeleteDrawing={handleDeleteDrawing}
            onEraseDrawings={handleEraseDrawings}
          />
        )}
      </section>
    </div>
  );
}

function CompareSymbolPicker({
  symbol,
  symbols,
  onSelect,
}: {
  symbol: string;
  symbols: string[];
  onSelect: (sym: string) => void;
}) {
  const { t } = useI18n();
  const marketAllLabel = t('watch.marketAll');
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const [popupPos, setPopupPos] = useState<{
    top: number;
    left: number;
    maxListHeight: number;
  } | null>(null);
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const popupRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);

  const updatePopupPos = useCallback(() => {
    const trigger = triggerRef.current;
    if (!trigger) return;
    const rect = trigger.getBoundingClientRect();
    const width = 224;
    const margin = 8;
    const headerHeight = 36;
    const spaceBelow = window.innerHeight - rect.bottom - margin;
    const spaceAbove = rect.top - margin;
    const openUp = spaceBelow < 180 && spaceAbove > spaceBelow;
    const available = Math.max(120, Math.min(280, (openUp ? spaceAbove : spaceBelow) - 6));
    const maxListHeight = Math.max(84, available - headerHeight);
    const top = openUp
      ? Math.max(margin, rect.top - available - 6)
      : Math.min(window.innerHeight - margin - available, rect.bottom + 6);
    const left = Math.max(
      margin,
      Math.min(window.innerWidth - width - margin, rect.left),
    );
    setPopupPos({ top, left, maxListHeight });
  }, []);

  useEffect(() => {
    if (!open) return;
    setQuery('');
    updatePopupPos();
    const timer = window.setTimeout(() => inputRef.current?.focus(), 10);
    const onPointer = (e: MouseEvent) => {
      const target = e.target as Node | null;
      if (
        (popupRef.current && target && popupRef.current.contains(target)) ||
        (triggerRef.current && target && triggerRef.current.contains(target))
      ) {
        return;
      }
      setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopImmediatePropagation();
        setOpen(false);
        requestAnimationFrame(() => triggerRef.current?.focus());
      }
    };
    const onViewport = () => updatePopupPos();
    window.addEventListener('mousedown', onPointer);
    window.addEventListener('keydown', onKey, { capture: true });
    window.addEventListener('resize', onViewport);
    window.addEventListener('scroll', onViewport, true);
    return () => {
      window.clearTimeout(timer);
      window.removeEventListener('mousedown', onPointer);
      window.removeEventListener('keydown', onKey, { capture: true });
      window.removeEventListener('resize', onViewport);
      window.removeEventListener('scroll', onViewport, true);
    };
  }, [open, updatePopupPos]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = q
      ? symbols.filter((s) => cleanSymbol(s, marketAllLabel).toLowerCase().includes(q))
      : symbols;
    return list.slice(0, 60);
  }, [symbols, query, marketAllLabel]);

  return (
    <div className="relative shrink-0">
      <button
        ref={triggerRef}
        type="button"
        aria-expanded={open}
        aria-haspopup="listbox"
        onClick={() => setOpen((v) => !v)}
        className="kairos-btn flex items-center gap-1 rounded-[5px] border px-2 py-0.5 font-mono text-xs font-semibold tabular-nums"
        style={{
          backgroundColor: 'var(--bg-canvas)',
          borderColor: open ? 'var(--accent-brand)' : 'var(--border-hairline)',
          color: 'var(--text-primary)',
        }}
        title={t('watch.comparePicker.tooltip')}
      >
        <span>{cleanSymbol(symbol, marketAllLabel)}</span>
        <ChevronDown className="h-3 w-3" style={{ color: 'var(--text-muted)' }} />
      </button>

      {open && popupPos && typeof document !== 'undefined'
        ? createPortal(
            <div
              ref={popupRef}
              className="kairos-floating kairos-animate-popover fixed z-[70] w-56 overflow-hidden rounded-[8px]"
              style={{
                top: `${popupPos.top}px`,
                left: `${popupPos.left}px`,
              }}
            >
              <div
                className="flex items-center gap-1.5 border-b px-2 py-1.5"
                style={{ borderColor: 'var(--border-hairline)' }}
              >
                <Search className="h-3 w-3 shrink-0" style={{ color: 'var(--text-muted)' }} />
                <input
                  ref={inputRef}
                  type="text"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && filtered.length > 0) {
                      e.preventDefault();
                      onSelect(filtered[0]);
                      setOpen(false);
                    }
                  }}
                  placeholder={t('watch.comparePicker.searchPlaceholder')}
                  className="w-full bg-transparent font-mono text-xs outline-none"
                  style={{ color: 'var(--text-primary)' }}
                />
              </div>
              <div
                role="listbox"
                className="overflow-y-auto p-1"
                style={{ maxHeight: `${popupPos.maxListHeight}px` }}
              >
                {filtered.length === 0 ? (
                  <div
                    className="px-2 py-3 text-center text-[11px]"
                    style={{ color: 'var(--text-muted)' }}
                  >
                    {t('watch.comparePicker.empty')}
                  </div>
                ) : (
                  filtered.map((s) => {
                    const active = sameSymbol(s, symbol);
                    return (
                      <button
                        key={s}
                        type="button"
                        role="option"
                        aria-selected={active}
                        onClick={() => {
                          onSelect(s);
                          setOpen(false);
                        }}
                        className="kairos-press flex w-full items-center justify-between rounded px-2 py-1 text-left font-mono text-xs tabular-nums transition-colors hover:bg-[var(--bg-hover)]"
                        style={{
                          backgroundColor: active ? 'var(--bg-elevated)' : 'transparent',
                          color: active ? 'var(--accent-brand-text)' : 'var(--text-primary)',
                        }}
                      >
                        <span>{cleanSymbol(s, marketAllLabel)}</span>
                        {active ? (
                          <span className="text-[10px]">{t('watch.comparePicker.current')}</span>
                        ) : null}
                      </button>
                    );
                  })
                )}
              </div>
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}
