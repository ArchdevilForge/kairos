import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { ChevronDown, Columns2, Search, X } from 'lucide-react';
import KairosChart from '../chart/KairosChart';
import { syncChartGroup, type SyncChartHandle } from '../chart/syncCharts';
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
import { PHASE_LABEL, sameSymbol } from '../utils/doctrine';
import { fmtDateTimeSec, fmtIsoUtc8, fmtPctPoints } from '../utils/format';

const HOTKEY_TFS: readonly Timeframe[] = ['1m', '5m', '15m', '1h', '4h'];

type QueueTab = 'all' | 'pulse' | 'major';

type QueueItem =
  | { kind: 'notice'; id: string; symbol: string; ts: number; notice: Notice }
  | { kind: 'anomaly'; id: string; symbol: string; ts: number; anomaly: AnomalyItem };

interface MoverChip {
  symbol: string;
  returnPct: number | null;
}

const ANOMALY_TYPE_LABEL: Record<string, string> = {
  price_velocity: '价格异动',
  volume_spike: '放量异动',
  open_interest_change: '持仓异动',
  funding_rate_anomaly: '费率异动',
  resonance: '多维共振',
};

interface ChartSliceState {
  bars: Bar[];
  metrics: MetricsPoint[];
  marketCycle: CycleMap | null;
  symbolCycle: CycleMap | null;
  loading: boolean;
  loadingMore?: boolean;
  error: string | null;
}

const EMPTY_SLICE: ChartSliceState = {
  bars: [],
  metrics: [],
  marketCycle: null,
  symbolCycle: null,
  loading: false,
  loadingMore: false,
  error: null,
};

function mergeBarsAscending(older: Bar[], current: Bar[]): Bar[] {
  const byTime = new Map<number, Bar>();
  for (const b of older) byTime.set(b.time, b);
  for (const b of current) byTime.set(b.time, b);
  return Array.from(byTime.values()).sort((a, b) => a.time - b.time);
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

function cleanSymbol(sym: string): string {
  if (sym.trim().toUpperCase() === 'MARKET') return '全市场';
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

function marketAnomalyMeta(a: AnomalyItem): {
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

  let title = '大盘共振异动';
  let tone: 'bull' | 'bear' | 'warn' | 'muted' = 'muted';
  switch (a.event_type) {
    case 'market_stress':
      title = isDown ? '大盘极端下跌' : '大盘极端上攻';
      tone = isDown ? 'bear' : 'warn';
      break;
    case 'market_trend':
      title = isDown ? '大盘延续下跌' : '大盘延续上攻';
      tone = isDown ? 'bear' : 'bull';
      break;
    case 'market_impulse':
      title = isDown ? '大盘脉冲下杀' : '大盘脉冲上攻';
      tone = isDown ? 'bear' : 'bull';
      break;
    case 'market_decay':
      title = '大盘脉冲衰减';
      tone = 'muted';
      break;
    case 'market_data_stale':
      title = '大盘行情数据异常';
      tone = 'warn';
      break;
    case 'market_data_recovered':
      title = '大盘行情恢复';
      tone = 'muted';
      break;
    default:
      title = isUp ? '大盘上攻异动' : isDown ? '大盘下杀异动' : '大盘共振异动';
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

function upsertBar(list: Bar[], incoming: Bar): Bar[] {
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

function shortNodeHint(label: string, node: CycleNode | null): string | null {
  if (!node) return null;
  const dir = node.direction === 'up' ? '多' : node.direction === 'down' ? '空' : '震荡';
  if (dir === '震荡') return `${label} 震荡`;
  return `${label} ${dir}·${PHASE_LABEL[node.phase]}`;
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
  // Queue drawer closed by default for 100% chart breathing room
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [queueTab, setQueueTab] = useState<QueueTab>('all');
  const [selectedQueueId, setSelectedQueueId] = useState<string>('');

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
      });
    } else {
      setPrimarySlice({ ...EMPTY_SLICE, loading: true });
    }

    client
      .getMarketBars({ symbol: activeSymbol, tf: activeTf, limit: 800 })
      .then((res) => {
        if (cancelled) return;
        const loaded = res.bars ?? [];
        if (loaded.length < 2) {
          primaryHistoryRef.current.exhausted = true;
        }
        const prevCached = barCacheRef.current.get(key);
        const mergedBars =
          prevCached && prevCached.bars.length > 0 && loaded.length > 0
            ? mergeBarsAscending(prevCached.bars, loaded)
            : loaded;
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
        });
      })
      .catch((err) => {
        if (cancelled) return;
        setPrimarySlice((prev) => ({
          ...prev,
          loading: false,
          error: err instanceof Error ? err.message : '加载 K 线失败',
        }));
      });

    return () => {
      cancelled = true;
    };
  }, [client, activeSymbol, activeTf, writeBarCache]);

  // Load primary symbol drawings only when activeSymbol changes (never on timeframe switch)
  useEffect(() => {
    let cancelled = false;
    client
      .getDrawings(activeSymbol)
      .then((list) => {
        if (!cancelled) setDrawings(list);
      })
      .catch(() => {
        if (!cancelled) setDrawings([]);
      });
    return () => {
      cancelled = true;
    };
  }, [client, activeSymbol]);

  // Load bottom comparison chart bars when splitMode is ON (0ms instant from cache)
  useEffect(() => {
    if (!splitMode) return;
    let cancelled = false;
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
      });
    } else {
      setCompareSlice({ ...EMPTY_SLICE, loading: true });
    }

    client
      .getMarketBars({ symbol: compareSymbol, tf: compareTf, limit: 800 })
      .then((res) => {
        if (cancelled) return;
        const loaded = res.bars ?? [];
        if (loaded.length < 2) {
          compareHistoryRef.current.exhausted = true;
        }
        const prevCached = barCacheRef.current.get(key);
        const mergedBars =
          prevCached && prevCached.bars.length > 0 && loaded.length > 0
            ? mergeBarsAscending(prevCached.bars, loaded)
            : loaded;
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
        });
      })
      .catch((err) => {
        if (cancelled) return;
        setCompareSlice((prev) => ({
          ...prev,
          loading: false,
          error: err instanceof Error ? err.message : '加载对比 K 线失败',
        }));
      });

    return () => {
      cancelled = true;
    };
  }, [client, splitMode, compareSymbol, compareTf, writeBarCache]);

  // Load comparison symbol drawings only when compareSymbol changes
  useEffect(() => {
    if (!splitMode) return;
    let cancelled = false;
    client
      .getDrawings(compareSymbol)
      .then((list) => {
        if (!cancelled) setCompareDrawings(list);
      })
      .catch(() => {
        if (!cancelled) setCompareDrawings([]);
      });
    return () => {
      cancelled = true;
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

    const unsub = client.onWsEvent((evt) => {
      if (evt.type !== 'kline') return;
      if (sameSymbol(evt.symbol, activeSymbol) && evt.tf === activeTf) {
        setPrimarySlice((prev) => {
          if (prev.loading && prev.bars.length === 0) return prev;
          const nextBars = upsertBar(prev.bars, evt.bar);
          if (nextBars === prev.bars) return prev;
          writeBarCache(`${activeSymbol}|${activeTf}`, {
            bars: nextBars,
            marketCycle: prev.marketCycle,
            symbolCycle: prev.symbolCycle,
          });
          return { ...prev, bars: nextBars };
        });
      }
      if (splitMode && sameSymbol(evt.symbol, compareSymbol) && evt.tf === compareTf) {
        setCompareSlice((prev) => {
          if (prev.loading && prev.bars.length === 0) return prev;
          const nextBars = upsertBar(prev.bars, evt.bar);
          if (nextBars === prev.bars) return prev;
          writeBarCache(`${compareSymbol}|${compareTf}`, {
            bars: nextBars,
            marketCycle: prev.marketCycle,
            symbolCycle: prev.symbolCycle,
          });
          return { ...prev, bars: nextBars };
        });
      }
    });

    return () => {
      unsub();
    };
  }, [client, activeSymbol, activeTf, splitMode, compareSymbol, compareTf, writeBarCache]);

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
    } catch (err) {
      console.warn('清空画线失败', err);
    }
  }, [client, activeSymbol]);

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
    } catch (err) {
      console.warn('清空画线失败', err);
    }
  }, [client, compareSymbol]);

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
  const contextHint = shortNodeHint('4h', pickNode(symbolCycle, ['4h', '1d']));
  const setupHint = shortNodeHint('15m', pickNode(symbolCycle, ['15m', '1h']));
  const cycleHintText = [contextHint, setupHint].filter(Boolean).join(' · ');

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
          title="多周期结构参考"
        >
          {cycleHintText}
        </span>
      ) : null}

      <button
        type="button"
        onClick={onToggleSplit}
        title="双图同步联排 (快捷键 s)"
        className="flex items-center gap-1 rounded border px-1.5 py-0.5 text-[11px] transition-colors"
        style={{
          backgroundColor: splitMode ? 'var(--bg-elevated)' : 'transparent',
          borderColor: splitMode ? 'var(--accent-brand)' : 'var(--border-hairline)',
          color: splitMode ? 'var(--accent-brand)' : 'var(--text-secondary)',
        }}
      >
        <Columns2 className="h-3 w-3" />
        <span>双图</span>
      </button>

      <button
        type="button"
        onClick={() => setDrawerOpen((v) => !v)}
        title="展开/收起异动队列"
        className="flex items-center gap-1 rounded border px-1.5 py-0.5 text-[11px] transition-colors"
        style={{
          backgroundColor: drawerOpen ? 'var(--bg-elevated)' : 'transparent',
          borderColor: drawerOpen ? 'var(--accent-brand)' : 'var(--border-hairline)',
          color: drawerOpen ? 'var(--accent-brand)' : 'var(--text-secondary)',
        }}
      >
        <span>异动</span>
        {queueItems.length > 0 ? (
          <span
            className="inline-block h-1.5 w-1.5 rounded-full"
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
      {/* Left Slide-Over Queue Drawer (Hidden by default) */}
      {drawerOpen ? (
        <aside
          className="absolute top-0 bottom-0 left-0 z-20 flex w-[260px] flex-col border-r shadow-xl"
          style={{
            backgroundColor: 'var(--bg-surface)',
            borderColor: 'var(--border-strong)',
          }}
        >
          <div
            className="flex items-center justify-between border-b px-3 py-2"
            style={{ borderColor: 'var(--border-hairline)' }}
          >
            <div className="flex gap-1 text-[11px]">
              {(
                [
                  ['all', '全部'],
                  ['pulse', '大盘共振'],
                  ['major', '龙头异动'],
                ] as const
              ).map(([id, label]) => (
                <button
                  key={id}
                  type="button"
                  onClick={() => setQueueTab(id)}
                  className="rounded px-2 py-0.5 transition-colors"
                  style={{
                    backgroundColor:
                      queueTab === id ? 'var(--bg-elevated)' : 'transparent',
                    color:
                      queueTab === id ? 'var(--accent-brand)' : 'var(--text-secondary)',
                  }}
                >
                  {label}
                </button>
              ))}
            </div>
            <button
              type="button"
              onClick={() => setDrawerOpen(false)}
              className="rounded p-1 hover:opacity-80"
              style={{ color: 'var(--text-muted)' }}
              aria-label="关闭队列"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>

          <div className="min-h-0 flex-1 space-y-1 overflow-y-auto p-1.5">
            {queueItems.length === 0 ? (
              <div
                className="px-3 py-10 text-center text-xs"
                style={{ color: 'var(--text-muted)' }}
              >
                暂无大盘共振与异动事件
              </div>
            ) : (
              queueItems.map((item) => {
                const isSelected = item.id === selectedQueueId;
                return (
                  <div
                    key={item.id}
                    onClick={() => selectQueueItem(item)}
                    className="w-full cursor-pointer rounded border px-2.5 py-2 text-left transition-colors"
                    style={{
                      backgroundColor: isSelected
                        ? 'var(--bg-elevated)'
                        : 'var(--bg-canvas)',
                      borderColor: isSelected
                        ? 'var(--accent-brand)'
                        : 'var(--border-hairline)',
                    }}
                  >
                    {item.kind === 'notice' ? (
                      <>
                        <div className="flex items-center justify-between gap-1">
                          <span className="font-mono text-xs font-medium tabular-nums" style={{ color: 'var(--text-primary)' }}>
                            {cleanSymbol(item.notice.symbol)}
                          </span>
                          <span className="text-[10px]" style={{ color: 'var(--accent-brand)' }}>
                            提示
                          </span>
                        </div>
                        <div className="mt-1 line-clamp-2 text-[11px]" style={{ color: 'var(--text-secondary)' }}>
                          {item.notice.trigger_facts.fact_summary ||
                            ANOMALY_TYPE_LABEL[item.notice.trigger_facts.event_type] ||
                            item.notice.trigger_facts.event_type}
                        </div>
                        <div className="mt-0.5 font-mono text-[10px] tabular-nums" style={{ color: 'var(--text-muted)' }}>
                          {fmtIsoUtc8(item.notice.ts)}
                        </div>
                      </>
                    ) : isMarketAnomaly(item.anomaly) ? (
                      (() => {
                        const meta = marketAnomalyMeta(item.anomaly);
                        const titleColor =
                          meta.tone === 'bear'
                            ? 'var(--pnl-neg)'
                            : meta.tone === 'bull'
                              ? 'var(--pnl-pos)'
                              : meta.tone === 'warn'
                                ? 'var(--state-warn)'
                                : 'var(--text-primary)';
                        return (
                          <>
                            <div className="flex items-center justify-between gap-1">
                              <span className="text-xs font-medium" style={{ color: titleColor }}>
                                {meta.title}
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
                                  覆盖率{' '}
                                  {(() => {
                                    const cov = asFiniteNumber(item.anomaly.data?.coverage);
                                    return cov != null ? `${Math.round(cov * 100)}%` : '—';
                                  })()}
                                </span>
                                <span>
                                  有效 {asFiniteNumber(item.anomaly.data?.valid_symbols) ?? '—'}
                                </span>
                              </div>
                            ) : (
                              <div
                                className="mt-1 flex items-center justify-between font-mono text-[11px] tabular-nums"
                                style={{ color: 'var(--text-secondary)' }}
                              >
                                <span>
                                  60s 中位涨跌{' '}
                                  <span
                                    style={{
                                      color:
                                        meta.med60 == null
                                          ? 'var(--text-muted)'
                                          : meta.med60 >= 0
                                            ? 'var(--pnl-pos)'
                                            : 'var(--pnl-neg)',
                                    }}
                                  >
                                    {meta.med60 != null ? fmtPctPoints(meta.med60, 2, true) : '—'}
                                  </span>
                                </span>
                                <span>
                                  广度{' '}
                                  {meta.breadthRatio != null
                                    ? `${Math.round(meta.breadthRatio * 100)}%`
                                    : '—'}
                                </span>
                              </div>
                            )}
                            {meta.chips.length > 0 ? (
                              <div className="mt-1.5 flex flex-wrap gap-1">
                                {meta.chips.map((chip) => (
                                  <button
                                    key={chip.symbol}
                                    type="button"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      setSelectedQueueId(item.id);
                                      onSelectSymbol(chip.symbol);
                                    }}
                                    className="tabular-nums flex items-center gap-1 rounded border px-1.5 py-0.5 text-[10px] transition-colors hover:opacity-85"
                                    style={{
                                      backgroundColor: 'var(--bg-surface)',
                                      borderColor: 'var(--border-hairline)',
                                      color: 'var(--text-primary)',
                                    }}
                                    title={`切换主图至 ${cleanSymbol(chip.symbol)}`}
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
                        const label =
                          ANOMALY_TYPE_LABEL[item.anomaly.event_type] ?? item.anomaly.event_type;
                        return (
                          <>
                            <div className="flex items-center justify-between gap-1">
                              <span className="font-mono text-xs font-medium tabular-nums" style={{ color: 'var(--text-primary)' }}>
                                {cleanSymbol(item.anomaly.symbol)}
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
                            <div className="mt-0.5 flex items-center justify-between font-mono text-[10px] tabular-nums" style={{ color: 'var(--text-muted)' }}>
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
      ) : null}

      {/* Main 100% Full-Bleed Chart Stage */}
      <section className="flex min-w-0 flex-1 flex-col">
        {splitMode ? (
          <div
            className="grid min-h-0 flex-1 grid-rows-2 divide-y"
            style={{ borderColor: 'var(--border-hairline)' }}
          >
            <KairosChart
              chartId="watch-primary"
              symbol={activeSymbol}
              timeframe={activeTf}
              availableTimeframes={['1m', '5m', '15m', '1h', '4h']}
              onTimeframeChange={onSelectTf}
              headerBadge="主图"
              statusSlot={chartControls}
              bars={primarySlice.bars}
              metrics={primarySlice.metrics}
              anomalies={drawerOpen ? symbolAnomalies : []}
              notices={drawerOpen ? visibleSymbolNotices : []}
              drawings={drawings}
              loading={primarySlice.loading}
              loadingMore={Boolean(primarySlice.loadingMore)}
              error={primarySlice.error}
              followLatest
              onLoadMoreHistory={loadMorePrimary}
              onUserDrawing={handleUserDrawing}
              onUpdateDrawing={handleUpdateDrawing}
              onDeleteDrawing={handleDeleteDrawing}
              onEraseDrawings={handleEraseDrawings}
              onChartReady={setPrimaryHandle}
            />
            <KairosChart
              chartId="watch-compare"
              symbol={compareSymbol}
              timeframe={compareTf}
              availableTimeframes={['1m', '5m', '15m', '1h', '4h']}
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
              headerBadge="对照"
              onClosePane={onToggleSplit}
              bars={compareSlice.bars}
              metrics={compareSlice.metrics}
              drawings={compareDrawings}
              loading={compareSlice.loading}
              loadingMore={Boolean(compareSlice.loadingMore)}
              error={compareSlice.error}
              followLatest
              onLoadMoreHistory={loadMoreCompare}
              onUserDrawing={handleCompareDrawing}
              onUpdateDrawing={handleUpdateCompareDrawing}
              onDeleteDrawing={handleDeleteCompareDrawing}
              onEraseDrawings={handleEraseCompareDrawings}
              onChartReady={setCompareHandle}
            />
          </div>
        ) : (
          <KairosChart
            chartId="watch-single"
            symbol={activeSymbol}
            timeframe={activeTf}
            availableTimeframes={['1m', '5m', '15m', '1h', '4h']}
            onTimeframeChange={onSelectTf}
            statusSlot={chartControls}
            bars={primarySlice.bars}
            metrics={primarySlice.metrics}
            anomalies={drawerOpen ? symbolAnomalies : []}
            notices={drawerOpen ? visibleSymbolNotices : []}
            drawings={drawings}
            loading={primarySlice.loading}
            loadingMore={Boolean(primarySlice.loadingMore)}
            error={primarySlice.error}
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
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState('');
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    if (!open) return;
    setQuery('');
    const t = window.setTimeout(() => inputRef.current?.focus(), 10);
    const onPointer = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    window.addEventListener('mousedown', onPointer);
    window.addEventListener('keydown', onKey);
    return () => {
      window.clearTimeout(t);
      window.removeEventListener('mousedown', onPointer);
      window.removeEventListener('keydown', onKey);
    };
  }, [open]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = q
      ? symbols.filter((s) => cleanSymbol(s).toLowerCase().includes(q))
      : symbols;
    return list.slice(0, 60);
  }, [symbols, query]);

  return (
    <div ref={wrapRef} className="relative shrink-0">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1 rounded border px-2 py-0.5 font-mono text-xs font-semibold tabular-nums transition-colors"
        style={{
          backgroundColor: 'var(--bg-canvas)',
          borderColor: open ? 'var(--accent-brand)' : 'var(--border-hairline)',
          color: 'var(--text-primary)',
        }}
        title="切换对照 Crypto 永续合约"
      >
        <span>{cleanSymbol(symbol)}</span>
        <ChevronDown className="h-3 w-3" style={{ color: 'var(--text-muted)' }} />
      </button>

      {open ? (
        <div
          className="absolute top-full left-0 z-40 mt-1 w-56 overflow-hidden rounded-md border shadow-xl"
          style={{
            backgroundColor: 'var(--bg-surface)',
            borderColor: 'var(--border-strong)',
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
              placeholder="搜索 Crypto 永续…"
              className="w-full bg-transparent font-mono text-xs outline-none"
              style={{ color: 'var(--text-primary)' }}
            />
          </div>
          <div className="max-h-60 overflow-y-auto p-1">
            {filtered.length === 0 ? (
              <div className="px-2 py-3 text-center text-[11px]" style={{ color: 'var(--text-muted)' }}>
                无匹配 Crypto 永续合约
              </div>
            ) : (
              filtered.map((s) => {
                const active = sameSymbol(s, symbol);
                return (
                  <button
                    key={s}
                    type="button"
                    onClick={() => {
                      onSelect(s);
                      setOpen(false);
                    }}
                    className="flex w-full items-center justify-between rounded px-2 py-1 text-left font-mono text-xs tabular-nums transition-colors"
                    style={{
                      backgroundColor: active ? 'var(--bg-elevated)' : 'transparent',
                      color: active ? 'var(--accent-brand)' : 'var(--text-primary)',
                    }}
                  >
                    <span>{cleanSymbol(s)}</span>
                    {active ? <span className="text-[10px]">当前</span> : null}
                  </button>
                );
              })
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}
