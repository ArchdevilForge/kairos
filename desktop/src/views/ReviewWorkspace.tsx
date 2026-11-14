import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  Anchor,
  ChevronLeft,
  ChevronRight,
  List,
  SkipBack,
  SkipForward,
  X,
} from 'lucide-react';
import KairosChart, { type ChartPriceLineSpec } from '../chart/KairosChart';
import {
  type Bar,
  type ChartDrawingItem,
  type ChartDrawingPoint,
  type DrawingKind,
  type MetricsPoint,
  type PositionSnapshot,
  type ReplayStateResponse,
  type ReviewWorkspaceProps,
  type SyncedTrade,
  type TicketBundle,
  type Timeframe,
  type TradeFillItem,
} from '../types/kairos';
import { sameSymbol } from '../utils/doctrine';
import {
  TIMEFRAME_SECONDS,
  fmtDateTimeSec,
  fmtDateTimeWithSec,
  fmtPrice,
  fmtR,
  fmtUsd,
} from '../utils/format';

type SelectorTab = 'all' | 'executed' | 'skipped';

type SelectedSubject =
  | { kind: 'ticket'; id: string; bundle: TicketBundle }
  | { kind: 'trade'; id: string; trade: SyncedTrade }
  | { kind: 'position'; id: string; position: PositionSnapshot };

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  if (target.isContentEditable) return true;
  const tag = target.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT';
}

function cleanSymbol(sym: string): string {
  return sym.replace(/:USDT$/, '');
}

export function ReviewWorkspace({
  client,
  bootstrap,
  initialSymbol,
  initialCursorTs,
  initialTicketId,
  onSelectSymbol,
}: ReviewWorkspaceProps) {
  const [selectorOpen, setSelectorOpen] = useState(false);
  const [selectorTab, setSelectorTab] = useState<SelectorTab>('all');

  const [symbol, setSymbol] = useState<string>(
    initialSymbol || bootstrap.status.symbols[0] || 'BTC/USDT:USDT',
  );
  const [activeTf, setActiveTf] = useState<Timeframe>('15m');

  const [selectedKey, setSelectedKey] = useState<string>(() =>
    initialTicketId ? `ticket:${initialTicketId}` : '',
  );

  // Full bars & drawings
  const [fullBars, setFullBars] = useState<Bar[]>([]);
  const [fullMetrics, setFullMetrics] = useState<MetricsPoint[]>([]);
  const [fullDrawings, setFullDrawings] = useState<ChartDrawingItem[]>([]);
  const [barsLoading, setBarsLoading] = useState(false);
  const [loadingMoreBars, setLoadingMoreBars] = useState(false);
  const [barsError, setBarsError] = useState<string | null>(null);
  const reviewHistoryRef = useRef<{ key: string; inFlight: boolean; exhausted: boolean }>({
    key: '',
    inFlight: false,
    exhausted: false,
  });
  const reviewBarCacheRef = useRef<Map<string, Bar[]>>(new Map());

  // Zero-lookahead Bar-Replay state (Hide Future off until enabled or replaying)
  const [hideFuture, setHideFuture] = useState<boolean>(false);
  const [cursorTs, setCursorTs] = useState<number>(() =>
    initialCursorTs && initialCursorTs > 0
      ? initialCursorTs
      : Math.floor(Date.now() / 1000),
  );
  const [playing, setPlaying] = useState(false);
  const [playSpeed, setPlaySpeed] = useState(5);
  const [stepBars, setStepBars] = useState(1);
  const [replayState, setReplayState] = useState<ReplayStateResponse | null>(null);
  const [replayError, setReplayError] = useState<string | null>(null);

  // Build unified list of reviewable subjects sorted by time descending
  const allSubjects = useMemo<SelectedSubject[]>(() => {
    const items: Array<{ ts: number; subj: SelectedSubject }> = [];
    for (const b of bootstrap.tickets ?? []) {
      items.push({
        ts: b.ticket.signal_at || b.ticket.created_at,
        subj: { kind: 'ticket', id: b.ticket.id, bundle: b },
      });
    }
    for (const tr of bootstrap.unplanned_trades ?? []) {
      items.push({
        ts: Math.floor(tr.entry_time_ms / 1000),
        subj: { kind: 'trade', id: tr.id, trade: tr },
      });
    }
    for (const pos of bootstrap.open_positions ?? []) {
      items.push({
        ts: pos.updated_at || Math.floor(Date.now() / 1000),
        subj: {
          kind: 'position',
          id: `${pos.symbol}:${pos.position_side}`,
          position: pos,
        },
      });
    }
    items.sort((a, b) => b.ts - a.ts);
    return items.map((x) => x.subj);
  }, [bootstrap.tickets, bootstrap.unplanned_trades, bootstrap.open_positions]);

  const filteredSubjects = useMemo(() => {
    if (selectorTab === 'all') return allSubjects;
    if (selectorTab === 'executed') {
      return allSubjects.filter((s) => {
        if (s.kind === 'position') return true;
        if (s.kind === 'trade') return false;
        const dec = s.bundle.decision?.decision ?? s.bundle.ticket.status;
        return dec === 'accepted' || s.bundle.ticket.status === 'closed';
      });
    }
    return allSubjects.filter((s) => {
      if (s.kind === 'trade') return true;
      if (s.kind === 'ticket') {
        const dec = s.bundle.decision?.decision ?? s.bundle.ticket.status;
        return dec === 'rejected' || dec === 'missed' || dec === 'waiting';
      }
      return false;
    });
  }, [allSubjects, selectorTab]);

  // Resolve current SelectedSubject
  const selectedSubject = useMemo<SelectedSubject | null>(() => {
    if (selectedKey.startsWith('ticket:')) {
      const id = selectedKey.slice('ticket:'.length);
      const b = bootstrap.tickets.find((x) => x.ticket.id === id);
      if (b) return { kind: 'ticket', id, bundle: b };
    } else if (selectedKey.startsWith('trade:')) {
      const id = selectedKey.slice('trade:'.length);
      const t = bootstrap.unplanned_trades.find((x) => x.id === id);
      if (t) return { kind: 'trade', id, trade: t };
    } else if (selectedKey.startsWith('position:')) {
      const id = selectedKey.slice('position:'.length);
      const p = bootstrap.open_positions.find(
        (x) => `${x.symbol}:${x.position_side}` === id,
      );
      if (p) return { kind: 'position', id, position: p };
    }

    if (initialTicketId) {
      const b = bootstrap.tickets.find((x) => x.ticket.id === initialTicketId);
      if (b) return { kind: 'ticket', id: b.ticket.id, bundle: b };
    }
    return allSubjects[0] ?? null;
  }, [
    selectedKey,
    bootstrap.tickets,
    bootstrap.unplanned_trades,
    bootstrap.open_positions,
    initialTicketId,
    allSubjects,
  ]);

  useEffect(() => {
    if (initialSymbol) {
      setSymbol(initialSymbol);
    }
    if (initialTicketId) {
      setSelectedKey(`ticket:${initialTicketId}`);
      const match = bootstrap.tickets.find((b) => b.ticket.id === initialTicketId);
      if (match) {
        setSymbol(match.ticket.symbol);
        const anchor =
          initialCursorTs ||
          match.ticket.entry_triggered_at ||
          match.ticket.signal_at ||
          match.ticket.created_at;
        if (anchor > 0) setCursorTs(anchor);
      } else if (initialCursorTs && initialCursorTs > 0) {
        setCursorTs(initialCursorTs);
      }
    } else if (initialCursorTs && initialCursorTs > 0) {
      setCursorTs(initialCursorTs);
    }
  }, [initialSymbol, initialCursorTs, initialTicketId, bootstrap.tickets]);

  const anchorTimestampFor = useCallback((subj: SelectedSubject): number => {
    if (subj.kind === 'ticket') {
      return (
        subj.bundle.ticket.entry_triggered_at ||
        subj.bundle.ticket.signal_at ||
        subj.bundle.ticket.created_at ||
        Math.floor(Date.now() / 1000)
      );
    }
    if (subj.kind === 'trade') {
      return Math.floor(subj.trade.entry_time_ms / 1000);
    }
    return subj.position.updated_at || Math.floor(Date.now() / 1000);
  }, []);

  const handleSelectSubject = useCallback(
    (subj: SelectedSubject) => {
      setPlaying(false);
      const nextSym =
        subj.kind === 'ticket'
          ? subj.bundle.ticket.symbol
          : subj.kind === 'trade'
            ? subj.trade.symbol
            : subj.position.symbol;
      setSelectedKey(`${subj.kind}:${subj.id}`);
      setSymbol(nextSym);
      onSelectSymbol(nextSym);
      setCursorTs(anchorTimestampFor(subj));
    },
    [anchorTimestampFor, onSelectSymbol],
  );

  const stepSubject = useCallback(
    (delta: -1 | 1) => {
      if (filteredSubjects.length === 0) return;
      const idx = selectedSubject
        ? filteredSubjects.findIndex(
            (s) => s.kind === selectedSubject.kind && s.id === selectedSubject.id,
          )
        : -1;
      const nextIdx =
        idx < 0
          ? 0
          : Math.max(0, Math.min(filteredSubjects.length - 1, idx + delta));
      handleSelectSubject(filteredSubjects[nextIdx]);
    },
    [filteredSubjects, selectedSubject, handleSelectSubject],
  );

  // Load market bars (0ms instant from cache)
  useEffect(() => {
    let cancelled = false;
    const key = `${symbol}|${activeTf}`;
    reviewHistoryRef.current = { key, inFlight: false, exhausted: false };
    setLoadingMoreBars(false);
    setBarsError(null);
    const cached = reviewBarCacheRef.current.get(key);
    if (cached && cached.length > 0) {
      setFullBars(cached);
      setBarsLoading(false);
    } else {
      setBarsLoading(true);
    }
    client
      .getMarketBars({ symbol, tf: activeTf, limit: 800 })
      .then((barsRes) => {
        if (cancelled) return;
        const bars = barsRes.bars ?? [];
        if (bars.length < 2) reviewHistoryRef.current.exhausted = true;
        reviewBarCacheRef.current.set(key, bars);
        setFullBars(bars);
        setFullMetrics(barsRes.metrics ?? []);
        setBarsLoading(false);

        if (bars.length > 0) {
          const minT = bars[0].time;
          const maxT = bars[bars.length - 1].time;
          setCursorTs((prev) => {
            if (prev <= 0 || prev < minT || prev > maxT) {
              return maxT;
            }
            return prev;
          });
        }
      })
      .catch((err) => {
        if (cancelled) return;
        setBarsLoading(false);
        setBarsError(err instanceof Error ? err.message : '加载复盘 K 线失败');
      });
    return () => {
      cancelled = true;
    };
  }, [client, symbol, activeTf]);

  // Load drawings only when symbol changes (never on timeframe switch)
  useEffect(() => {
    let cancelled = false;
    client
      .getDrawings(symbol)
      .then((drawList) => {
        if (!cancelled) setFullDrawings(drawList);
      })
      .catch(() => {
        if (!cancelled) setFullDrawings([]);
      });
    return () => {
      cancelled = true;
    };
  }, [client, symbol]);

  const handleLoadMoreHistory = useCallback(
    (oldestTimeSec: number) => {
      if (hideFuture || oldestTimeSec <= 0) return;
      const key = `${symbol}|${activeTf}`;
      if (reviewHistoryRef.current.key !== key) {
        reviewHistoryRef.current = { key, inFlight: false, exhausted: false };
      }
      if (reviewHistoryRef.current.inFlight || reviewHistoryRef.current.exhausted) {
        return;
      }
      reviewHistoryRef.current.inFlight = true;
      setLoadingMoreBars(true);
      client
        .getMarketBars({ symbol, tf: activeTf, limit: 800, beforeTs: oldestTimeSec })
        .then((res) => {
          if (reviewHistoryRef.current.key !== key) return;
          const older = (res.bars ?? []).filter((b) => b.time < oldestTimeSec);
          if (older.length < 2) {
            reviewHistoryRef.current.exhausted = true;
          }
          if (older.length > 0) {
            setFullBars((prev) => {
              const byTime = new Map<number, Bar>();
              for (const b of older) byTime.set(b.time, b);
              for (const b of prev) byTime.set(b.time, b);
              return Array.from(byTime.values()).sort((a, b) => a.time - b.time);
            });
          }
        })
        .catch(() => {
          // Ignore transient pagination error.
        })
        .finally(() => {
          if (reviewHistoryRef.current.key === key) {
            reviewHistoryRef.current.inFlight = false;
            setLoadingMoreBars(false);
          }
        });
    },
    [client, hideFuture, symbol, activeTf],
  );

  // Fetch zero-lookahead replay state whenever Hide Future is ON
  useEffect(() => {
    if (!hideFuture || cursorTs <= 0) return;
    let cancelled = false;
    setReplayError(null);
    client
      .getReplayState({
        symbol,
        cursorTs,
        tfs: [activeTf],
        limit: 300,
      })
      .then((res) => {
        if (cancelled) return;
        setReplayState(res);
      })
      .catch((err) => {
        if (cancelled) return;
        setReplayError(err instanceof Error ? err.message : '回放截断接口失败');
      });
    return () => {
      cancelled = true;
    };
  }, [client, hideFuture, symbol, activeTf, cursorTs]);

  const minCursorTs = fullBars[0]?.time ?? Math.max(0, cursorTs - 86400);
  const maxCursorTs =
    fullBars[fullBars.length - 1]?.time ?? Math.floor(Date.now() / 1000);
  const tfStepSec = TIMEFRAME_SECONDS[activeTf] ?? 900;

  const stepCursor = useCallback(
    (delta: 1 | -1) => {
      setCursorTs((prev) => {
        const next = prev + delta * stepBars * tfStepSec;
        return Math.max(minCursorTs, Math.min(maxCursorTs, next));
      });
    },
    [stepBars, tfStepSec, minCursorTs, maxCursorTs],
  );

  // Playback timer
  useEffect(() => {
    if (!playing) return;
    const intervalMs = Math.max(50, Math.round(1000 / Math.max(1, playSpeed)));
    const id = window.setInterval(() => {
      setCursorTs((prev) => {
        const next = prev + stepBars * tfStepSec;
        if (next >= maxCursorTs) {
          setPlaying(false);
          return maxCursorTs;
        }
        return next;
      });
    }, intervalMs);
    return () => window.clearInterval(id);
  }, [playing, playSpeed, stepBars, tfStepSec, maxCursorTs]);

  // Anomaly jump timestamps (`a` prev / `n` next)
  const symbolAnomalyTimes = useMemo(() => {
    const times = new Set<number>();
    for (const a of bootstrap.anomalies) {
      if (sameSymbol(a.symbol, symbol)) times.add(a.timestamp);
    }
    for (const b of bootstrap.tickets) {
      if (sameSymbol(b.ticket.symbol, symbol) && b.ticket.signal_at > 0) {
        times.add(b.ticket.signal_at);
      }
    }
    return [...times].sort((a, b) => a - b);
  }, [bootstrap.anomalies, bootstrap.tickets, symbol]);

  const jumpAnomaly = useCallback(
    (dir: -1 | 1) => {
      if (symbolAnomalyTimes.length === 0) return;
      if (dir === -1) {
        const prev = [...symbolAnomalyTimes]
          .reverse()
          .find((t) => t < cursorTs - 1);
        if (prev != null) setCursorTs(prev);
      } else {
        const next = symbolAnomalyTimes.find((t) => t > cursorTs + 1);
        if (next != null) setCursorTs(next);
      }
    },
    [symbolAnomalyTimes, cursorTs],
  );

  const handleToggleHideFuture = useCallback(() => {
    setPlaying(false);
    setHideFuture((prev) => {
      const next = !prev;
      if (next && fullBars.length > 60) {
        const latestBar = fullBars[fullBars.length - 1].time;
        if (cursorTs >= latestBar - tfStepSec) {
          const anchor = fullBars[Math.max(0, fullBars.length - 60)].time;
          setCursorTs(anchor);
        }
      }
      return next;
    });
  }, [fullBars, cursorTs, tfStepSec]);

  // Hotkeys: Space play/pause, Left/Right step, a/n anomaly jump, Esc close selector
  const replayKeyRef = useRef({
    playing,
    stepCursor,
    jumpAnomaly,
    selectorOpen,
  });
  replayKeyRef.current = { playing, stepCursor, jumpAnomaly, selectorOpen };

  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.metaKey || e.ctrlKey || e.altKey) return;
      if (e.key === 'Escape') {
        if (replayKeyRef.current.selectorOpen) {
          e.preventDefault();
          setSelectorOpen(false);
          return;
        }
      }
      if (isEditableTarget(e.target)) return;

      const lower = e.key.toLowerCase();
      if (e.code === 'Space') {
        e.preventDefault();
        setPlaying((p) => !p);
        return;
      }
      if (e.key === 'ArrowLeft') {
        e.preventDefault();
        replayKeyRef.current.stepCursor(-1);
        return;
      }
      if (e.key === 'ArrowRight') {
        e.preventDefault();
        replayKeyRef.current.stepCursor(1);
        return;
      }
      if (lower === 'a') {
        e.preventDefault();
        replayKeyRef.current.jumpAnomaly(-1);
        return;
      }
      if (lower === 'n') {
        e.preventDefault();
        replayKeyRef.current.jumpAnomaly(1);
      }
    };
    window.addEventListener('keydown', onKeyDown);
    return () => {
      window.removeEventListener('keydown', onKeyDown);
    };
  }, []);

  // Zero-lookahead visible data passed to KairosChart
  const chartBars = useMemo<Bar[]>(() => {
    if (hideFuture) return replayState?.bars_by_tf?.[activeTf] ?? [];
    return fullBars;
  }, [hideFuture, replayState, activeTf, fullBars]);

  const chartDrawings = useMemo<ChartDrawingItem[]>(() => {
    if (hideFuture) return replayState?.drawings ?? [];
    return fullDrawings;
  }, [hideFuture, replayState, fullDrawings]);

  const activeFills = useMemo<TradeFillItem[]>(() => {
    if (hideFuture) return replayState?.fills ?? [];
    if (selectedSubject?.kind === 'ticket') {
      return selectedSubject.bundle.fills ?? selectedSubject.bundle.linked_trade?.fills ?? [];
    }
    if (selectedSubject?.kind === 'trade') {
      return selectedSubject.trade.fills ?? [];
    }
    return [];
  }, [hideFuture, replayState, selectedSubject]);

  const futureCount = useMemo(() => {
    if (!hideFuture || fullBars.length === 0) return null;
    return { shown: chartBars.length, total: fullBars.length };
  }, [hideFuture, fullBars.length, chartBars.length]);

  const reviewPriceLines = useMemo<ChartPriceLineSpec[]>(() => {
    if (!selectedSubject) return [];
    const lines: ChartPriceLineSpec[] = [];
    if (selectedSubject.kind === 'ticket') {
      const t = selectedSubject.bundle.ticket;
      const entry =
        selectedSubject.bundle.decision?.planned_entry ?? t.risk_plan.entry_price;
      if (entry > 0) {
        lines.push({
          id: 'entry',
          price: entry,
          title: '入场',
          color: '#D97757',
          draggable: false,
        });
      }
      const stopVal =
        selectedSubject.bundle.decision?.planned_stop ?? t.risk_plan.stop_price;
      if (stopVal != null && stopVal > 0) {
        lines.push({
          id: 'stop',
          price: stopVal,
          title: '止损',
          color: '#B85450',
          dashed: true,
          draggable: false,
        });
      }
    } else if (selectedSubject.kind === 'trade') {
      const tr = selectedSubject.trade;
      if (tr.entry_price > 0) {
        lines.push({ id: 'entry', price: tr.entry_price, title: '入场', color: '#D97757' });
      }
      if (tr.exit_price > 0 && (!hideFuture || tr.exit_time_ms / 1000 <= cursorTs)) {
        lines.push({
          id: 'exit',
          price: tr.exit_price,
          title: '出场',
          color: '#6A9BCC',
          dashed: true,
        });
      }
    } else if (selectedSubject.kind === 'position') {
      const pos = selectedSubject.position;
      if (pos.entry_price > 0) {
        lines.push({ id: 'entry', price: pos.entry_price, title: '均价', color: '#D97757' });
      }
    }
    return lines;
  }, [selectedSubject, hideFuture, cursorTs]);

  const handleUserDrawing = useCallback(
    async (kind: DrawingKind, points: ChartDrawingPoint[]) => {
      const nowSec = Math.floor(Date.now() / 1000);
      const tempId = -Date.now() - Math.floor(Math.random() * 1000);
      const optimistic: ChartDrawingItem = {
        id: tempId,
        symbol,
        kind,
        points,
        created_at: nowSec,
        updated_at: nowSec,
      };
      setFullDrawings((prev) => [...prev, optimistic]);
      if (hideFuture) {
        setReplayState((prev) =>
          prev ? { ...prev, drawings: [...prev.drawings, optimistic] } : prev,
        );
      }
      try {
        const created = await client.createDrawing({ symbol, kind, points });
        const replaceTemp = (list: ChartDrawingItem[]) =>
          list.map((d) => (d.id === tempId ? { ...created, points: d.points } : d));
        setFullDrawings(replaceTemp);
        if (hideFuture) {
          setReplayState((prev) =>
            prev ? { ...prev, drawings: replaceTemp(prev.drawings) } : prev,
          );
        }
      } catch {
        // Ignore transient failure.
      }
    },
    [client, symbol, hideFuture],
  );

  const handleDeleteDrawing = useCallback(
    async (id: number) => {
      setFullDrawings((prev) => prev.filter((d) => d.id !== id));
      setReplayState((prev) =>
        prev
          ? { ...prev, drawings: prev.drawings.filter((d) => d.id !== id) }
          : prev,
      );
      if (id <= 0) return;
      try {
        await client.deleteDrawing(id);
      } catch {
        // Ignore.
      }
    },
    [client],
  );

  const handleUpdateDrawing = useCallback(
    async (id: number, points: ChartDrawingPoint[]) => {
      const patch = (list: ChartDrawingItem[]) =>
        list.map((d) => (d.id === id ? { ...d, points } : d));
      setFullDrawings(patch);
      setReplayState((prev) => (prev ? { ...prev, drawings: patch(prev.drawings) } : prev));
      if (id <= 0) return;
      try {
        const updated = await client.updateDrawing(id, { points });
        const apply = (list: ChartDrawingItem[]) =>
          list.map((d) => (d.id === id ? updated : d));
        setFullDrawings(apply);
        setReplayState((prev) => (prev ? { ...prev, drawings: apply(prev.drawings) } : prev));
      } catch {
        // Ignore transient failure.
      }
    },
    [client],
  );

  const handleEraseDrawings = useCallback(async () => {
    setFullDrawings([]);
    setReplayState((prev) => (prev ? { ...prev, drawings: [] } : prev));
    try {
      await client.deleteDrawingsForSymbol(symbol);
    } catch {
      // Ignore.
    }
  }, [client, symbol]);

  const activeTicket =
    selectedSubject?.kind === 'ticket' ? selectedSubject.bundle : null;
  const activeTrade =
    selectedSubject?.kind === 'trade'
      ? selectedSubject.trade
      : activeTicket?.linked_trade ?? null;
  const activePosition =
    selectedSubject?.kind === 'position' ? selectedSubject.position : null;

  // Compact active subject summary badge
  const summaryBadge = useMemo(() => {
    if (!selectedSubject) return null;
    if (activeTrade) {
      const dir =
        activeTrade.direction === 'short' || activeTrade.direction === 'down'
          ? '做空'
          : '做多';
      return {
        text: `${dir} ${fmtUsd(activeTrade.net_pnl, true)}`,
        positive: activeTrade.net_pnl >= 0,
      };
    }
    if (activeTicket) {
      const dir = activeTicket.ticket.direction === 'down' ? '做空' : '做多';
      const r = activeTicket.outcome ? ` ${fmtR(activeTicket.outcome.net_r)}` : '';
      return {
        text: `${dir}${r}`,
        positive: (activeTicket.outcome?.net_r ?? 0) >= 0,
      };
    }
    if (activePosition) {
      return {
        text: `持仓 ${fmtUsd(activePosition.unrealized_pnl, true)}`,
        positive: activePosition.unrealized_pnl >= 0,
      };
    }
    return null;
  }, [selectedSubject, activeTrade, activeTicket, activePosition]);

  // Inline KISS controls inside KairosChart toolbar
  const reviewToolbarSlot = (
    <div className="flex shrink-0 items-center gap-1.5">
      <button
        type="button"
        onClick={() => setSelectorOpen((v) => !v)}
        className="flex items-center gap-1 rounded border px-2 py-0.5 text-[11px] transition-colors"
        style={{
          backgroundColor: selectorOpen ? 'var(--bg-elevated)' : 'transparent',
          borderColor: selectorOpen ? 'var(--accent-brand)' : 'var(--border-hairline)',
          color: selectorOpen ? 'var(--accent-brand)' : 'var(--text-primary)',
        }}
      >
        <List className="h-3 w-3" />
        <span className="tabular-nums">记录列表 ({allSubjects.length})</span>
      </button>

      {filteredSubjects.length > 0 ? (
        <div className="flex items-center gap-0.5">
          <button
            type="button"
            title="上一条记录"
            onClick={() => stepSubject(-1)}
            className="rounded p-0.5 hover:opacity-80"
            style={{ color: 'var(--text-secondary)' }}
          >
            <ChevronLeft className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            title="下一条记录"
            onClick={() => stepSubject(1)}
            className="rounded p-0.5 hover:opacity-80"
            style={{ color: 'var(--text-secondary)' }}
          >
            <ChevronRight className="h-3.5 w-3.5" />
          </button>
        </div>
      ) : null}

      {summaryBadge ? (
        <span
          className="rounded px-1.5 py-0.5 font-mono text-[11px] font-medium tabular-nums"
          style={{
            backgroundColor: summaryBadge.positive
              ? 'var(--candle-bull-soft)'
              : 'var(--candle-bear-soft)',
            color: summaryBadge.positive ? 'var(--pnl-pos)' : 'var(--pnl-neg)',
          }}
        >
          {summaryBadge.text}
        </span>
      ) : allSubjects.length === 0 ? (
        <span className="text-[11px]" style={{ color: 'var(--text-muted)' }}>
          暂无复盘记录
        </span>
      ) : null}
    </div>
  );

  return (
    <div
      className="relative flex h-full min-h-0 w-full overflow-hidden"
      style={{ backgroundColor: 'var(--bg-canvas)' }}
    >
      {/* Left Slide-Over Record Selector (Hidden by default) */}
      {selectorOpen ? (
        <aside
          className="absolute top-0 bottom-0 left-0 z-20 flex w-[272px] flex-col border-r shadow-xl"
          style={{
            backgroundColor: 'var(--bg-surface)',
            borderColor: 'var(--border-strong)',
          }}
        >
          <div
            className="flex items-center justify-between border-b px-3 py-2"
            style={{ borderColor: 'var(--border-hairline)' }}
          >
            <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>
              复盘记录
            </span>
            <button
              type="button"
              onClick={() => setSelectorOpen(false)}
              className="rounded p-1 hover:opacity-80"
              style={{ color: 'var(--text-muted)' }}
              aria-label="关闭记录列表"
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>

          <div
            className="grid grid-cols-3 gap-1 border-b p-1.5 text-[11px]"
            style={{ borderColor: 'var(--border-hairline)' }}
          >
            {(
              [
                ['all', '全部'],
                ['executed', '已执行'],
                ['skipped', '跳过与无票'],
              ] as const
            ).map(([id, label]) => (
              <button
                key={id}
                type="button"
                onClick={() => setSelectorTab(id)}
                className="rounded py-1 font-medium"
                style={{
                  backgroundColor:
                    selectorTab === id ? 'var(--bg-elevated)' : 'transparent',
                  color:
                    selectorTab === id ? 'var(--accent-brand)' : 'var(--text-secondary)',
                }}
              >
                {label}
              </button>
            ))}
          </div>

          <div className="min-h-0 flex-1 space-y-1 overflow-y-auto p-1.5">
            {filteredSubjects.length === 0 ? (
              <div
                className="px-3 py-10 text-center text-xs"
                style={{ color: 'var(--text-muted)' }}
              >
                暂无匹配记录
              </div>
            ) : (
              filteredSubjects.map((subj) => {
                const active =
                  selectedSubject?.kind === subj.kind && selectedSubject?.id === subj.id;
                const subjKey = `${subj.kind}:${subj.id}`;
                if (subj.kind === 'ticket') {
                  const b = subj.bundle;
                  const dir = b.ticket.direction === 'down' ? '做空' : '做多';
                  return (
                    <button
                      key={subjKey}
                      type="button"
                      onClick={() => handleSelectSubject(subj)}
                      className="w-full rounded border px-2.5 py-2 text-left transition-colors"
                      style={{
                        backgroundColor: active
                          ? 'var(--bg-elevated)'
                          : 'var(--bg-canvas)',
                        borderColor: active
                          ? 'var(--accent-brand)'
                          : 'var(--border-hairline)',
                      }}
                    >
                      <div className="flex items-center justify-between gap-1">
                        <span className="font-mono text-xs font-semibold tabular-nums" style={{ color: 'var(--text-primary)' }}>
                          {cleanSymbol(b.ticket.symbol)}
                        </span>
                        <span
                          className="rounded px-1.5 py-0.5 font-mono text-[10px] tabular-nums"
                          style={{
                            backgroundColor:
                              b.ticket.direction === 'up'
                                ? 'var(--candle-bull-soft)'
                                : 'var(--candle-bear-soft)',
                            color:
                              b.ticket.direction === 'up'
                                ? 'var(--pnl-pos)'
                                : 'var(--pnl-neg)',
                          }}
                        >
                          {dir} · {b.ticket.grade}
                        </span>
                      </div>
                      <div className="mt-1 flex items-center justify-between font-mono text-[10px] tabular-nums" style={{ color: 'var(--text-muted)' }}>
                        <span>{fmtDateTimeSec(b.ticket.signal_at || b.ticket.created_at)}</span>
                        {b.outcome ? (
                          <span
                            style={{
                              color:
                                b.outcome.net_r >= 0
                                  ? 'var(--pnl-pos)'
                                  : 'var(--pnl-neg)',
                            }}
                          >
                            {fmtR(b.outcome.net_r)}
                          </span>
                        ) : null}
                      </div>
                    </button>
                  );
                }
                if (subj.kind === 'trade') {
                  const tr = subj.trade;
                  const dir =
                    tr.direction === 'short' || tr.direction === 'down' ? '做空' : '做多';
                  return (
                    <button
                      key={subjKey}
                      type="button"
                      onClick={() => handleSelectSubject(subj)}
                      className="w-full rounded border px-2.5 py-2 text-left"
                      style={{
                        backgroundColor: active
                          ? 'var(--bg-elevated)'
                          : 'var(--bg-canvas)',
                        borderColor: active
                          ? 'var(--state-warn)'
                          : 'var(--border-hairline)',
                      }}
                    >
                      <div className="flex items-center justify-between">
                        <span className="font-mono text-xs font-semibold tabular-nums" style={{ color: 'var(--text-primary)' }}>
                          {cleanSymbol(tr.symbol)}
                        </span>
                        <span
                          className="rounded px-1.5 py-0.5 text-[10px]"
                          style={{
                            backgroundColor: 'var(--state-warn-soft)',
                            color: 'var(--state-warn)',
                          }}
                        >
                          无票开单
                        </span>
                      </div>
                      <div className="mt-1 flex items-center justify-between font-mono text-[10px] tabular-nums" style={{ color: 'var(--text-secondary)' }}>
                        <span>
                          {dir} {tr.leverage}x
                        </span>
                        <span style={{ color: tr.net_pnl >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)' }}>
                          {fmtUsd(tr.net_pnl, true)}
                        </span>
                      </div>
                    </button>
                  );
                }
                const pos = subj.position;
                return (
                  <button
                    key={subjKey}
                    type="button"
                    onClick={() => handleSelectSubject(subj)}
                    className="w-full rounded border px-2.5 py-2 text-left"
                    style={{
                      backgroundColor: active
                        ? 'var(--bg-elevated)'
                        : 'var(--bg-canvas)',
                      borderColor: active
                        ? 'var(--accent-info)'
                        : 'var(--border-hairline)',
                    }}
                  >
                    <div className="flex items-center justify-between">
                      <span className="font-mono text-xs font-semibold" style={{ color: 'var(--text-primary)' }}>
                        {cleanSymbol(pos.symbol)}
                      </span>
                      <span className="font-mono text-[10px] tabular-nums" style={{ color: 'var(--accent-info)' }}>
                        持仓 {pos.leverage}x
                      </span>
                    </div>
                    <div className="mt-1 flex items-center justify-between font-mono text-[10px] tabular-nums" style={{ color: 'var(--text-secondary)' }}>
                      <span>均价 {fmtPrice(pos.entry_price)}</span>
                      <span style={{ color: pos.unrealized_pnl >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)' }}>
                        {fmtUsd(pos.unrealized_pnl, true)}
                      </span>
                    </div>
                  </button>
                );
              })
            )}
          </div>
        </aside>
      ) : null}

      {/* Main Full-Bleed Replay Chart */}
      <section className="flex min-w-0 flex-1 flex-col">
        {hideFuture ? (
          <div
            className="flex shrink-0 items-center gap-2 border-b px-3 py-1 text-[11px]"
            style={{
              backgroundColor: 'var(--bg-surface)',
              borderColor: 'var(--border-hairline)',
            }}
          >
            <span className="font-mono tabular-nums" style={{ color: 'var(--accent-brand)' }}>
              {fmtDateTimeWithSec(cursorTs)}
            </span>
            <input
              type="range"
              aria-label="回放时间游标"
              min={minCursorTs}
              max={Math.max(minCursorTs + 60, maxCursorTs)}
              step={Math.min(60, tfStepSec)}
              value={Math.min(Math.max(cursorTs, minCursorTs), Math.max(minCursorTs + 60, maxCursorTs))}
              onChange={(e) => {
                setPlaying(false);
                setCursorTs(Number(e.target.value));
              }}
              className="h-1 flex-1 accent-[var(--accent-brand)]"
            />
            <button
              type="button"
              title="上一条异动 (a)"
              onClick={() => jumpAnomaly(-1)}
              className="rounded p-0.5 hover:opacity-80"
              style={{ color: 'var(--text-secondary)' }}
            >
              <SkipBack className="h-3 w-3" />
            </button>
            <button
              type="button"
              title="下一条异动 (n)"
              onClick={() => jumpAnomaly(1)}
              className="rounded p-0.5 hover:opacity-80"
              style={{ color: 'var(--text-secondary)' }}
            >
              <SkipForward className="h-3 w-3" />
            </button>
            {selectedSubject ? (
              <button
                type="button"
                title="回到交易锚点时间"
                onClick={() => {
                  setPlaying(false);
                  setCursorTs(anchorTimestampFor(selectedSubject));
                }}
                className="flex items-center gap-1 rounded px-1.5 py-0.5 hover:opacity-85"
                style={{ color: 'var(--text-secondary)' }}
              >
                <Anchor className="h-3 w-3" />
                <span>回锚点</span>
              </button>
            ) : null}
          </div>
        ) : null}

        <KairosChart
          chartId="review-replay"
          symbol={symbol}
          timeframe={activeTf}
          availableTimeframes={['1m', '5m', '15m', '1h', '4h']}
          onTimeframeChange={setActiveTf}
          statusSlot={reviewToolbarSlot}
          bars={chartBars}
          metrics={hideFuture ? [] : fullMetrics}
          direction={
            activeTicket
              ? activeTicket.ticket.direction
              : activeTrade
                ? activeTrade.direction === 'short' || activeTrade.direction === 'down'
                  ? 'down'
                  : 'up'
                : undefined
          }
          priceLines={reviewPriceLines}
          fills={activeFills}
          ticketSignalAt={activeTicket?.ticket.signal_at ?? null}
          drawings={chartDrawings}
          loading={barsLoading}
          loadingMore={loadingMoreBars}
          error={replayError ?? barsError}
          onLoadMoreHistory={hideFuture ? undefined : handleLoadMoreHistory}
          cursorTs={hideFuture ? cursorTs : null}
          onCursorDrag={(ts) => {
            setPlaying(false);
            setCursorTs(ts);
          }}
          onStepCursor={stepCursor}
          playing={playing}
          onTogglePlay={() => setPlaying((p) => !p)}
          playSpeed={playSpeed}
          onPlaySpeedChange={setPlaySpeed}
          stepBars={stepBars}
          onStepBarsChange={setStepBars}
          hideFuture={hideFuture}
          onToggleHideFuture={handleToggleHideFuture}
          futureCount={futureCount}
          followLatest={playing}
          onUserDrawing={handleUserDrawing}
          onUpdateDrawing={handleUpdateDrawing}
          onDeleteDrawing={handleDeleteDrawing}
          onEraseDrawings={handleEraseDrawings}
        />
      </section>
    </div>
  );
}
