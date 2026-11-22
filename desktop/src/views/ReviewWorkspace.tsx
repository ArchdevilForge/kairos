import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { Time } from 'lightweight-charts';
import {
  Anchor,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ChevronUp,
  List,
  SkipBack,
  SkipForward,
  X,
} from 'lucide-react';
import KairosChart, {
  type ChartPriceLineSpec,
  type ChartTradeSegmentInput,
} from '../chart/KairosChart';
import { SegmentedControl, type SegmentedOption } from '../components/SegmentedControl';
import {
  type Bar,
  type BootstrapPayload,
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
import { useI18n } from '../i18n';
import { sameSymbol } from '../utils/doctrine';
import {
  TIMEFRAME_SECONDS,
  fmtDateTimeSec,
  fmtDateTimeWithSec,
  fmtDurationSec,
  fmtPctPoints,
  fmtPrice,
  fmtR,
  fmtUsd,
} from '../utils/format';

type SelectorTab = 'all' | 'positions' | 'closed' | 'tickets';

const REVIEW_TIMEFRAMES: readonly Timeframe[] = ['1m', '5m', '15m', '1h', '4h'];

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

function baseCoin(sym: string): string {
  return sym.replace(/\/USDT(:USDT)?$/i, '').replace(/USDT$/i, '');
}

function fmtCompactQty(v: number | null | undefined): string {
  if (v == null || !Number.isFinite(v)) return '—';
  const abs = Math.abs(v);
  if (abs === 0) return '0';
  const decimals = abs >= 1000 ? 1 : abs >= 10 ? 2 : abs >= 1 ? 3 : 4;
  return Number(abs.toFixed(decimals)).toString();
}

function fmtLev(lev: number | null | undefined): string {
  if (lev == null || !Number.isFinite(lev) || lev <= 0) return '1x';
  return `${Number(lev.toFixed(2))}x`;
}

function findMatchingOpenTrade(
  bootstrap: BootstrapPayload,
  pos: PositionSnapshot,
): SyncedTrade | null {
  return (
    (bootstrap.unplanned_trades ?? []).find(
      (tr) =>
        tr.status === 'open' &&
        sameSymbol(tr.symbol, pos.symbol) &&
        (!pos.position_side ||
          !tr.position_side ||
          tr.position_side === pos.position_side),
    ) ?? null
  );
}

function buildReviewSubjects(bootstrap: BootstrapPayload): SelectedSubject[] {
  const items: Array<{ ts: number; priority: number; subj: SelectedSubject }> = [];
  const openPosKeys = new Set<string>();
  for (const pos of bootstrap.open_positions ?? []) {
    openPosKeys.add(`${pos.symbol}|${pos.position_side}`);
    const linkedOpen = findMatchingOpenTrade(bootstrap, pos);
    const ts =
      (linkedOpen && linkedOpen.entry_time_ms > 0
        ? Math.floor(linkedOpen.entry_time_ms / 1000)
        : 0) ||
      pos.updated_at ||
      Math.floor(Date.now() / 1000);
    items.push({
      ts,
      priority: 1,
      subj: {
        kind: 'position',
        id: `${pos.symbol}:${pos.position_side}`,
        position: pos,
      },
    });
  }
  for (const b of bootstrap.tickets ?? []) {
    items.push({
      ts: b.ticket.signal_at || b.ticket.created_at,
      priority: 0,
      subj: { kind: 'ticket', id: b.ticket.id, bundle: b },
    });
  }
  for (const tr of bootstrap.unplanned_trades ?? []) {
    if (tr.status === 'open' && openPosKeys.has(`${tr.symbol}|${tr.position_side}`)) {
      continue;
    }
    items.push({
      ts: Math.floor((tr.exit_time_ms || tr.entry_time_ms) / 1000),
      priority: 0,
      subj: { kind: 'trade', id: tr.id, trade: tr },
    });
  }
  items.sort((a, b) => {
    if (a.priority !== b.priority) return b.priority - a.priority;
    return b.ts - a.ts;
  });
  return items.map((x) => x.subj);
}

function subjectSymbol(subj: SelectedSubject): string {
  if (subj.kind === 'ticket') return subj.bundle.ticket.symbol;
  if (subj.kind === 'trade') return subj.trade.symbol;
  return subj.position.symbol;
}

function subjectTimeBounds(
  subj: SelectedSubject,
  bootstrap?: BootstrapPayload,
): { entrySec: number; exitSec: number } {
  if (subj.kind === 'ticket') {
    const entrySec =
      subj.bundle.ticket.entry_triggered_at ||
      subj.bundle.ticket.signal_at ||
      subj.bundle.ticket.created_at ||
      0;
    const exitSec =
      subj.bundle.ticket.closed_at ||
      (subj.bundle.linked_trade?.exit_time_ms
        ? Math.floor(subj.bundle.linked_trade.exit_time_ms / 1000)
        : entrySec);
    return { entrySec, exitSec };
  }
  if (subj.kind === 'trade') {
    const entrySec = Math.floor(subj.trade.entry_time_ms / 1000);
    const exitSec =
      subj.trade.exit_time_ms > 0 ? Math.floor(subj.trade.exit_time_ms / 1000) : entrySec;
    return { entrySec, exitSec };
  }
  const matched = bootstrap ? findMatchingOpenTrade(bootstrap, subj.position) : null;
  const entrySec =
    matched && matched.entry_time_ms > 0
      ? Math.floor(matched.entry_time_ms / 1000)
      : subj.position.updated_at || 0;
  const exitSec = subj.position.updated_at || entrySec;
  return { entrySec, exitSec };
}

function resolveInitialReviewTarget(
  bootstrap: BootstrapPayload,
  initialSymbol?: string,
  initialTicketId?: string,
  initialSubjectKind?: 'ticket' | 'trade' | 'position',
  initialSubjectId?: string,
): { symbol: string; selectedKey: string; anchorSec: number | null } {
  const subjects = buildReviewSubjects(bootstrap);
  if (initialSubjectKind && initialSubjectId) {
    const exact = subjects.find(
      (s) => s.kind === initialSubjectKind && s.id === initialSubjectId,
    );
    if (exact) {
      const bounds = subjectTimeBounds(exact, bootstrap);
      return {
        symbol: subjectSymbol(exact),
        selectedKey: `${exact.kind}:${exact.id}`,
        anchorSec: bounds.entrySec > 0 ? bounds.entrySec : null,
      };
    }
    if (initialSubjectKind === 'trade') {
      const rawTrade = (bootstrap.unplanned_trades ?? []).find(
        (tr) => tr.id === initialSubjectId,
      );
      if (rawTrade) {
        return {
          symbol: rawTrade.symbol,
          selectedKey: `trade:${rawTrade.id}`,
          anchorSec:
            rawTrade.entry_time_ms > 0 ? Math.floor(rawTrade.entry_time_ms / 1000) : null,
        };
      }
    }
  }
  if (initialTicketId) {
    const match = subjects.find((s) => s.kind === 'ticket' && s.id === initialTicketId);
    if (match) {
      const bounds = subjectTimeBounds(match, bootstrap);
      return {
        symbol: subjectSymbol(match),
        selectedKey: `ticket:${match.id}`,
        anchorSec: bounds.entrySec > 0 ? bounds.entrySec : null,
      };
    }
  }
  if (initialSymbol) {
    const sameSymSubj = subjects.find((s) => sameSymbol(subjectSymbol(s), initialSymbol));
    if (sameSymSubj) {
      const bounds = subjectTimeBounds(sameSymSubj, bootstrap);
      return {
        symbol: subjectSymbol(sameSymSubj),
        selectedKey: `${sameSymSubj.kind}:${sameSymSubj.id}`,
        anchorSec: bounds.entrySec > 0 ? bounds.entrySec : null,
      };
    }
  }
  if (subjects.length > 0) {
    const first = subjects[0];
    const bounds = subjectTimeBounds(first, bootstrap);
    return {
      symbol: subjectSymbol(first),
      selectedKey: `${first.kind}:${first.id}`,
      anchorSec: bounds.entrySec > 0 ? bounds.entrySec : null,
    };
  }
  return {
    symbol: initialSymbol || bootstrap.status.symbols[0] || 'BTC/USDT:USDT',
    selectedKey: '',
    anchorSec: null,
  };
}

export function ReviewWorkspace({
  client,
  bootstrap,
  initialSymbol,
  initialCursorTs,
  initialTicketId,
  initialSubjectKind,
  initialSubjectId,
  onSelectSymbol,
}: ReviewWorkspaceProps) {
  const { t } = useI18n();
  const [selectorOpen, setSelectorOpen] = useState(false);
  const [selectorTab, setSelectorTab] = useState<SelectorTab>('all');
  const [detailExpanded, setDetailExpanded] = useState(true);
  const selectorToggleBtnRef = useRef<HTMLButtonElement | null>(null);
  const selectorCloseBtnRef = useRef<HTMLButtonElement | null>(null);
  const selectorDrawerRef = useRef<HTMLElement | null>(null);

  useEffect(() => {
    if (selectorOpen) {
      const timer = window.setTimeout(() => selectorCloseBtnRef.current?.focus(), 10);
      return () => window.clearTimeout(timer);
    }
    if (
      selectorDrawerRef.current &&
      document.activeElement &&
      selectorDrawerRef.current.contains(document.activeElement)
    ) {
      selectorToggleBtnRef.current?.focus();
    }
    return undefined;
  }, [selectorOpen]);

  const initialResolved = useMemo(
    () =>
      resolveInitialReviewTarget(
        bootstrap,
        initialSymbol,
        initialTicketId,
        initialSubjectKind,
        initialSubjectId,
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  const [symbol, setSymbol] = useState<string>(initialResolved.symbol);
  const [activeTf, setActiveTf] = useState<Timeframe>('15m');
  const [selectedKey, setSelectedKey] = useState<string>(initialResolved.selectedKey);

  // Full bars & drawings (keyed by sliceKey to eliminate cross-symbol stale bar races)
  const [barsSlice, setBarsSlice] = useState<{ key: string; bars: Bar[] }>({
    key: '',
    bars: [],
  });
  const [fullMetrics, setFullMetrics] = useState<MetricsPoint[]>([]);
  const [fullDrawings, setFullDrawings] = useState<ChartDrawingItem[]>([]);
  const [barsLoading, setBarsLoading] = useState(false);
  const [loadingMoreBars, setLoadingMoreBars] = useState(false);
  const [barsErrorState, setBarsErrorState] = useState<{ raw: string | null } | null>(null);
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
      : (initialResolved.anchorSec ?? Math.floor(Date.now() / 1000)),
  );
  const [playing, setPlaying] = useState(false);
  const [playSpeed, setPlaySpeed] = useState(5);
  const [stepBars, setStepBars] = useState(1);
  const [replayState, setReplayState] = useState<ReplayStateResponse | null>(null);
  const [replayErrorState, setReplayErrorState] = useState<{ raw: string | null } | null>(null);

  const barsError = barsErrorState
    ? barsErrorState.raw !== null
      ? barsErrorState.raw
      : t('review.loadBarsFailed')
    : null;
  const replayError = replayErrorState
    ? replayErrorState.raw !== null
      ? replayErrorState.raw
      : t('review.replayLoadFailed')
    : null;

  // Build unified list of reviewable subjects sorted by time descending
  const allSubjects = useMemo<SelectedSubject[]>(
    () => buildReviewSubjects(bootstrap),
    [bootstrap],
  );

  const filteredSubjects = useMemo(() => {
    if (selectorTab === 'all') return allSubjects;
    if (selectorTab === 'positions') {
      return allSubjects.filter(
        (s) => s.kind === 'position' || (s.kind === 'trade' && s.trade.status === 'open'),
      );
    }
    if (selectorTab === 'closed') {
      return allSubjects.filter(
        (s) => s.kind === 'trade' && s.trade.status !== 'open',
      );
    }
    return allSubjects.filter((s) => s.kind === 'ticket');
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

  const prevDrilldownRef = useRef<string>('');
  useEffect(() => {
    const sig = `${initialSubjectKind ?? ''}|${initialSubjectId ?? ''}|${initialTicketId ?? ''}|${initialCursorTs ?? ''}|${initialSymbol ?? ''}`;
    if (sig === prevDrilldownRef.current) return;
    prevDrilldownRef.current = sig;

    if (initialSubjectKind && initialSubjectId) {
      const exact = allSubjects.find(
        (s) => s.kind === initialSubjectKind && s.id === initialSubjectId,
      );
      setSelectedKey(`${initialSubjectKind}:${initialSubjectId}`);
      if (exact) {
        setSymbol(subjectSymbol(exact));
        const bounds = subjectTimeBounds(exact, bootstrap);
        const anchor =
          initialCursorTs && Number.isFinite(initialCursorTs) && initialCursorTs > 0
            ? initialCursorTs
            : bounds.entrySec;
        if (anchor > 0) setCursorTs(anchor);
        return;
      }
      if (initialSubjectKind === 'trade') {
        const rawTrade = (bootstrap.unplanned_trades ?? []).find(
          (tr) => tr.id === initialSubjectId,
        );
        if (rawTrade) {
          setSymbol(rawTrade.symbol);
          const anchor =
            (initialCursorTs && Number.isFinite(initialCursorTs) && initialCursorTs > 0
              ? initialCursorTs
              : 0) || Math.floor(rawTrade.entry_time_ms / 1000);
          if (anchor > 0) setCursorTs(anchor);
          return;
        }
      }
    }

    if (initialTicketId) {
      setSelectedKey(`ticket:${initialTicketId}`);
      const match = bootstrap.tickets.find((b) => b.ticket.id === initialTicketId);
      if (match) {
        setSymbol(match.ticket.symbol);
        const anchor =
          (initialCursorTs && Number.isFinite(initialCursorTs) && initialCursorTs > 0
            ? initialCursorTs
            : 0) ||
          match.ticket.entry_triggered_at ||
          match.ticket.signal_at ||
          match.ticket.created_at;
        if (anchor > 0) setCursorTs(anchor);
      } else if (initialCursorTs && Number.isFinite(initialCursorTs) && initialCursorTs > 0) {
        setCursorTs(initialCursorTs);
      }
    } else if (initialCursorTs && Number.isFinite(initialCursorTs) && initialCursorTs > 0) {
      if (initialSymbol) setSymbol(initialSymbol);
      setCursorTs(initialCursorTs);
    }
  }, [
    initialSymbol,
    initialCursorTs,
    initialTicketId,
    initialSubjectKind,
    initialSubjectId,
    bootstrap.tickets,
    allSubjects,
  ]);

  const anchorTimestampFor = useCallback(
    (subj: SelectedSubject): number => {
      return subjectTimeBounds(subj, bootstrap).entrySec || Math.floor(Date.now() / 1000);
    },
    [bootstrap],
  );

  const handleSelectSubject = useCallback(
    (subj: SelectedSubject) => {
      setPlaying(false);
      const nextSym = subjectSymbol(subj);
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

  const tfStepSec = TIMEFRAME_SECONDS[activeTf] ?? 900;
  const subjectMatchesSymbol =
    selectedSubject != null && sameSymbol(subjectSymbol(selectedSubject), symbol);
  const selectedBounds = useMemo(
    () =>
      subjectMatchesSymbol && selectedSubject
        ? subjectTimeBounds(selectedSubject, bootstrap)
        : { entrySec: 0, exitSec: 0 },
    [subjectMatchesSymbol, selectedSubject, bootstrap],
  );
  const windowTag =
    subjectMatchesSymbol && selectedSubject && selectedBounds.entrySec > 0
      ? `subj:${selectedSubject.kind}:${selectedSubject.id}`
      : 'latest';
  const sliceKey = `${symbol}|${activeTf}|${windowTag}`;
  const fullBars = useMemo<Bar[]>(() => {
    if (barsSlice.key === sliceKey) return barsSlice.bars;
    return reviewBarCacheRef.current.get(sliceKey) ?? [];
  }, [barsSlice, sliceKey]);

  // Load market bars (keyed by sliceKey + historical trade window backfill)
  useEffect(() => {
    let cancelled = false;
    const ac = new AbortController();
    reviewHistoryRef.current = { key: sliceKey, inFlight: false, exhausted: false };
    setLoadingMoreBars(false);
    setBarsErrorState(null);

    const cachedSlice = reviewBarCacheRef.current.get(sliceKey);
    if (cachedSlice && cachedSlice.length > 0) {
      setBarsSlice({ key: sliceKey, bars: cachedSlice });
      setBarsLoading(false);
    } else {
      setBarsLoading(true);
    }

    (async () => {
      try {
        const baseKey = `${symbol}|${activeTf}|latest`;
        let baseBars = reviewBarCacheRef.current.get(baseKey);
        let latestMetrics: MetricsPoint[] = [];
        if (!baseBars || baseBars.length === 0) {
          const barsRes = await client.getMarketBars({ symbol, tf: activeTf, limit: 800, signal: ac.signal });
          if (cancelled) return;
          baseBars = barsRes.bars ?? [];
          latestMetrics = barsRes.metrics ?? [];
          if (baseBars.length > 0) {
            reviewBarCacheRef.current.set(baseKey, baseBars);
          }
        }

        let finalBars = baseBars;
        if (
          selectedBounds.entrySec > 0 &&
          finalBars.length > 0 &&
          selectedBounds.entrySec < finalBars[0].time + 10 * tfStepSec
        ) {
          const targetEnd = Math.max(selectedBounds.exitSec, selectedBounds.entrySec);
          const histRes = await client.getMarketBars({
            symbol,
            tf: activeTf,
            limit: 800,
            beforeTs: targetEnd + 200 * tfStepSec,
            signal: ac.signal,
          });
          if (cancelled) return;
          if ((histRes.bars?.length ?? 0) > 0) {
            finalBars = histRes.bars;
          }
        }

        if (finalBars.length < 2) {
          reviewHistoryRef.current.exhausted = true;
        }
        reviewBarCacheRef.current.set(sliceKey, finalBars);
        setBarsSlice({ key: sliceKey, bars: finalBars });
        setFullMetrics(latestMetrics);
        setBarsLoading(false);

        if (finalBars.length > 0) {
          const minT = finalBars[0].time;
          const maxT = finalBars[finalBars.length - 1].time;
          setCursorTs((prev) => {
            if (prev <= 0 || prev < minT || prev > maxT) {
              return selectedBounds.entrySec >= minT && selectedBounds.entrySec <= maxT
                ? selectedBounds.entrySec
                : maxT;
            }
            return prev;
          });
        }
      } catch (err) {
        if (cancelled) return;
        setBarsLoading(false);
        setBarsErrorState({ raw: err instanceof Error ? err.message : null });
      }
    })();

    return () => {
      cancelled = true;
      ac.abort();
    };
  }, [
    client,
    symbol,
    activeTf,
    sliceKey,
    selectedBounds.entrySec,
    selectedBounds.exitSec,
    tfStepSec,
  ]);

  // Load drawings only when symbol changes (never on timeframe switch)
  useEffect(() => {
    let cancelled = false;
    const ac = new AbortController();
    client
      .getDrawings(symbol, ac.signal)
      .then((drawList) => {
        if (!cancelled) setFullDrawings(drawList);
      })
      .catch(() => {
        if (!cancelled) setFullDrawings([]);
      });
    return () => {
      cancelled = true;
      ac.abort();
    };
  }, [client, symbol]);

  const handleLoadMoreHistory = useCallback(
    (oldestTimeSec: number) => {
      if (hideFuture || oldestTimeSec <= 0) return;
      if (reviewHistoryRef.current.key !== sliceKey) {
        reviewHistoryRef.current = { key: sliceKey, inFlight: false, exhausted: false };
      }
      if (reviewHistoryRef.current.inFlight || reviewHistoryRef.current.exhausted) {
        return;
      }
      reviewHistoryRef.current.inFlight = true;
      setLoadingMoreBars(true);
      client
        .getMarketBars({ symbol, tf: activeTf, limit: 800, beforeTs: oldestTimeSec })
        .then((res) => {
          if (reviewHistoryRef.current.key !== sliceKey) return;
          const older = (res.bars ?? []).filter((b) => b.time < oldestTimeSec);
          if (older.length < 2) {
            reviewHistoryRef.current.exhausted = true;
          }
          if (older.length > 0) {
            setBarsSlice((prev) => {
              const curBars =
                prev.key === sliceKey ? prev.bars : (reviewBarCacheRef.current.get(sliceKey) ?? []);
              const byTime = new Map<number, Bar>();
              for (const b of older) byTime.set(b.time, b);
              for (const b of curBars) byTime.set(b.time, b);
              const nextBars = Array.from(byTime.values()).sort((a, b) => a.time - b.time);
              reviewBarCacheRef.current.set(sliceKey, nextBars);
              return { key: sliceKey, bars: nextBars };
            });
          }
        })
        .catch(() => {
          // Ignore transient pagination error.
        })
        .finally(() => {
          if (reviewHistoryRef.current.key === sliceKey) {
            reviewHistoryRef.current.inFlight = false;
            setLoadingMoreBars(false);
          }
        });
    },
    [client, hideFuture, symbol, activeTf, sliceKey],
  );

  // Fetch zero-lookahead replay state whenever Hide Future is ON
  useEffect(() => {
    if (!hideFuture || cursorTs <= 0) return;
    let cancelled = false;
    setReplayErrorState(null);
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
        setReplayErrorState({ raw: err instanceof Error ? err.message : null });
      });
    return () => {
      cancelled = true;
    };
  }, [client, hideFuture, symbol, activeTf, cursorTs]);

  const minCursorTs = fullBars[0]?.time ?? Math.max(0, cursorTs - 86400);
  const maxCursorTs =
    fullBars[fullBars.length - 1]?.time ?? Math.floor(Date.now() / 1000);

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
          e.stopImmediatePropagation();
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
    if (!subjectMatchesSymbol || chartBars.length === 0) return [];
    if (hideFuture) return replayState?.fills ?? [];
    if (selectedSubject?.kind === 'ticket') {
      return selectedSubject.bundle.fills ?? selectedSubject.bundle.linked_trade?.fills ?? [];
    }
    if (selectedSubject?.kind === 'trade') {
      return selectedSubject.trade.fills ?? [];
    }
    if (selectedSubject?.kind === 'position') {
      return findMatchingOpenTrade(bootstrap, selectedSubject.position)?.fills ?? [];
    }
    return [];
  }, [hideFuture, replayState, selectedSubject, subjectMatchesSymbol, chartBars.length, bootstrap]);

  const futureCount = useMemo(() => {
    if (!hideFuture || fullBars.length === 0) return null;
    return { shown: chartBars.length, total: fullBars.length };
  }, [hideFuture, fullBars.length, chartBars.length]);

  const reviewPriceLines = useMemo<ChartPriceLineSpec[]>(() => {
    if (!selectedSubject || !subjectMatchesSymbol || chartBars.length === 0) return [];
    const lines: ChartPriceLineSpec[] = [];
    if (selectedSubject.kind === 'ticket') {
      const ticket = selectedSubject.bundle.ticket;
      const entry =
        selectedSubject.bundle.decision?.planned_entry ?? ticket.risk_plan.entry_price;
      if (entry > 0) {
        lines.push({
          id: 'entry',
          price: entry,
          title: t('review.priceLine.entry'),
          color: '#D97757',
          draggable: false,
        });
      }
      const stopVal =
        selectedSubject.bundle.decision?.planned_stop ?? ticket.risk_plan.stop_price;
      if (stopVal != null && stopVal > 0) {
        lines.push({
          id: 'stop',
          price: stopVal,
          title: t('review.priceLine.stop'),
          color: '#B85450',
          dashed: true,
          draggable: false,
        });
      }
    } else if (selectedSubject.kind === 'trade') {
      const tr = selectedSubject.trade;
      if (tr.entry_price > 0) {
        lines.push({
          id: 'entry',
          price: tr.entry_price,
          title: t('review.priceLine.entry'),
          color: '#D97757',
        });
      }
      if (tr.exit_price > 0 && (!hideFuture || tr.exit_time_ms / 1000 <= cursorTs)) {
        lines.push({
          id: 'exit',
          price: tr.exit_price,
          title: t('review.priceLine.exit'),
          color: '#6A9BCC',
          dashed: true,
        });
      }
      if (tr.status === 'open' && tr.liq_price > 0) {
        lines.push({
          id: 'liq',
          price: tr.liq_price,
          title: t('review.priceLine.liq'),
          color: '#D49B4B',
          dashed: true,
        });
      }
    } else if (selectedSubject.kind === 'position') {
      const pos = selectedSubject.position;
      if (pos.entry_price > 0) {
        lines.push({
          id: 'entry',
          price: pos.entry_price,
          title: t('review.priceLine.avg'),
          color: '#D97757',
        });
      }
      if (pos.stop_loss != null && pos.stop_loss > 0) {
        lines.push({
          id: 'stop',
          price: pos.stop_loss,
          title: t('review.priceLine.stop'),
          color: '#B85450',
          dashed: true,
        });
      }
      if (pos.take_profit != null && pos.take_profit > 0) {
        lines.push({
          id: 'tp',
          price: pos.take_profit,
          title: t('review.priceLine.takeProfit'),
          color: '#5D9670',
          dashed: true,
        });
      }
      if (pos.liq_price > 0) {
        lines.push({
          id: 'liq',
          price: pos.liq_price,
          title: t('review.priceLine.liq'),
          color: '#D49B4B',
          dashed: true,
        });
      }
    }
    return lines;
  }, [selectedSubject, subjectMatchesSymbol, chartBars.length, hideFuture, cursorTs, t]);

  const reviewTradeSegment = useMemo<ChartTradeSegmentInput | null>(() => {
    if (!selectedSubject || !subjectMatchesSymbol || chartBars.length === 0) return null;
    if (selectedSubject.kind === 'trade') {
      const tr = selectedSubject.trade;
      if (tr.entry_price <= 0 || selectedBounds.entrySec <= 0) return null;
      const showExit =
        tr.exit_price > 0 &&
        selectedBounds.exitSec > 0 &&
        (!hideFuture || selectedBounds.exitSec <= cursorTs);
      return {
        entryTime: selectedBounds.entrySec,
        entryPrice: tr.entry_price,
        exitTime: showExit ? selectedBounds.exitSec : null,
        exitPrice: showExit ? tr.exit_price : null,
        profitable: tr.net_pnl >= 0,
      };
    }
    return null;
  }, [
    selectedSubject,
    subjectMatchesSymbol,
    chartBars.length,
    selectedBounds.entrySec,
    selectedBounds.exitSec,
    hideFuture,
    cursorTs,
  ]);

  const firstFullBarTime = fullBars[0]?.time ?? 0;
  const lastFullBarTime = fullBars[fullBars.length - 1]?.time ?? 0;
  const reviewVisibleRange = useMemo<{ from: Time; to: Time } | null>(() => {
    if (
      hideFuture ||
      !subjectMatchesSymbol ||
      selectedBounds.entrySec <= 0 ||
      firstFullBarTime <= 0 ||
      lastFullBarTime <= firstFullBarTime
    ) {
      return null;
    }
    if (
      selectedBounds.entrySec < firstFullBarTime - tfStepSec ||
      selectedBounds.entrySec > lastFullBarTime + tfStepSec
    ) {
      return null;
    }
    const span = Math.max(selectedBounds.exitSec - selectedBounds.entrySec, 16 * tfStepSec);
    const pad = Math.max(40 * tfStepSec, Math.round(span * 0.75));
    const from = Math.max(firstFullBarTime, selectedBounds.entrySec - pad);
    const to = Math.min(
      lastFullBarTime,
      Math.max(selectedBounds.exitSec, selectedBounds.entrySec) + pad,
    );
    if (from >= to) {
      return { from: firstFullBarTime as Time, to: lastFullBarTime as Time };
    }
    return { from: from as Time, to: to as Time };
  }, [
    hideFuture,
    subjectMatchesSymbol,
    selectedBounds.entrySec,
    selectedBounds.exitSec,
    tfStepSec,
    firstFullBarTime,
    lastFullBarTime,
  ]);

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
  const activePositionOpenTrade = useMemo(
    () => (activePosition ? findMatchingOpenTrade(bootstrap, activePosition) : null),
    [activePosition, bootstrap],
  );

  // Compact active subject summary badge
  const summaryBadge = useMemo(() => {
    if (!selectedSubject) return null;
    if (activeTrade) {
      const dir =
        activeTrade.direction === 'short' || activeTrade.direction === 'down'
          ? t('review.dir.short')
          : t('review.dir.long');
      return {
        text: `${dir} ${fmtLev(activeTrade.leverage)} ${fmtUsd(activeTrade.net_pnl, true)}`,
        positive: activeTrade.net_pnl >= 0,
      };
    }
    if (activeTicket) {
      const dir =
        activeTicket.ticket.direction === 'down'
          ? t('review.dir.short')
          : t('review.dir.long');
      const r = activeTicket.outcome ? ` ${fmtR(activeTicket.outcome.net_r)}` : '';
      return {
        text: `${dir}${r}`,
        positive: (activeTicket.outcome?.net_r ?? 0) >= 0,
      };
    }
    if (activePosition) {
      const dir =
        activePosition.qty < 0 ? t('review.dir.short') : t('review.dir.long');
      return {
        text: `${dir} ${fmtLev(activePosition.leverage)} ${fmtUsd(activePosition.unrealized_pnl, true)}`,
        positive: activePosition.unrealized_pnl >= 0,
      };
    }
    return null;
  }, [selectedSubject, activeTrade, activeTicket, activePosition, t]);

  const selectorTabOptions = useMemo<ReadonlyArray<SegmentedOption<SelectorTab>>>(
    () => [
      { value: 'all', label: t('review.selector.tabAll'), activeColor: 'var(--accent-brand)' },
      { value: 'positions', label: t('review.selector.tabPositions'), activeColor: 'var(--accent-info)' },
      { value: 'closed', label: t('review.selector.tabClosed'), activeColor: 'var(--accent-brand)' },
      { value: 'tickets', label: t('review.selector.tabTickets'), activeColor: 'var(--accent-brand)' },
    ],
    [t],
  );

  // Inline KISS controls inside KairosChart toolbar
  const reviewToolbarSlot = (
    <div className="flex shrink-0 items-center gap-1.5">
      <button
        ref={selectorToggleBtnRef}
        type="button"
        onClick={() => setSelectorOpen((v) => !v)}
        className="kairos-btn flex items-center gap-1 rounded-[5px] border px-2 py-0.5 text-[11px] font-medium"
        style={{
          backgroundColor: selectorOpen ? 'var(--accent-brand-soft)' : 'transparent',
          borderColor: selectorOpen ? 'var(--accent-brand)' : 'var(--border-hairline)',
          color: selectorOpen ? 'var(--accent-brand-text)' : 'var(--text-primary)',
        }}
      >
        <List className="h-3 w-3" />
        <span className="tabular-nums">
          {t('review.toolbar.recordList', { count: allSubjects.length })}
        </span>
      </button>

      {filteredSubjects.length > 0 ? (
        <div className="flex items-center gap-0.5">
          <button
            type="button"
            title={t('review.toolbar.prevRecord')}
            onClick={() => stepSubject(-1)}
            className="kairos-btn rounded border border-transparent p-0.5"
            style={{ color: 'var(--text-secondary)' }}
          >
            <ChevronLeft className="h-3.5 w-3.5" />
          </button>
          <button
            type="button"
            title={t('review.toolbar.nextRecord')}
            onClick={() => stepSubject(1)}
            className="kairos-btn rounded border border-transparent p-0.5"
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
          {t('review.toolbar.empty')}
        </span>
      ) : null}
    </div>
  );

  return (
    <div
      className="relative flex h-full min-h-0 w-full overflow-hidden"
      style={{ backgroundColor: 'var(--bg-canvas)' }}
    >
      {/* Left Slide-Over Record Selector (GPU slide transition) */}
      <div
        data-open={selectorOpen}
        className="kairos-drawer-scrim kairos-glass-backdrop absolute inset-0 z-10 sm:hidden"
        onClick={() => setSelectorOpen(false)}
        aria-hidden="true"
      />
      <aside
        ref={selectorDrawerRef}
        data-open={selectorOpen}
        aria-hidden={!selectorOpen}
        inert={!selectorOpen ? true : undefined}
        className="kairos-drawer-left kairos-contain-panel absolute top-0 bottom-0 left-0 z-20 flex w-[min(86vw,288px)] flex-col border-r sm:w-[276px]"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-strong)',
          boxShadow: selectorOpen ? 'var(--shadow-floating)' : 'none',
        }}
      >
        <div
          className="flex items-center justify-between border-b px-3 py-2"
          style={{ borderColor: 'var(--border-hairline)' }}
        >
          <span className="text-xs font-medium" style={{ color: 'var(--text-primary)' }}>
            {t('review.selector.title')}
          </span>
          <button
            ref={selectorCloseBtnRef}
            type="button"
            onClick={() => setSelectorOpen(false)}
            className="kairos-btn rounded-[5px] border border-transparent p-1"
            style={{ color: 'var(--text-muted)' }}
            aria-label={t('review.selector.close')}
          >
            <X className="h-3.5 w-3.5" />
          </button>
        </div>

        <div
          className="border-b p-2"
          style={{ borderColor: 'var(--border-hairline)' }}
        >
          <SegmentedControl<SelectorTab>
            options={selectorTabOptions}
            value={selectorTab}
            onChange={setSelectorTab}
            size="xs"
            fullWidth
          />
        </div>

        <div className="min-h-0 flex-1 space-y-1.5 overflow-y-auto p-2">
          {!selectorOpen ? null : filteredSubjects.length === 0 ? (
            <div
              className="px-3 py-10 text-center text-xs"
              style={{ color: 'var(--text-muted)' }}
            >
              {t('review.selector.emptyFiltered')}
            </div>
          ) : (
            filteredSubjects.map((subj) => {
              const active =
                selectedSubject?.kind === subj.kind && selectedSubject?.id === subj.id;
              const subjKey = `${subj.kind}:${subj.id}`;
              if (subj.kind === 'ticket') {
                const b = subj.bundle;
                const dir =
                  b.ticket.direction === 'down'
                    ? t('review.dir.short')
                    : t('review.dir.long');
                return (
                  <button
                    key={subjKey}
                    type="button"
                    tabIndex={selectorOpen ? 0 : -1}
                    onClick={() => handleSelectSubject(subj)}
                    className="kairos-list-item kairos-row w-full rounded-[6px] border px-2.5 py-2 text-left"
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
                      <span
                        className="font-mono text-xs font-semibold tabular-nums"
                        style={{ color: 'var(--text-primary)' }}
                      >
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
                    <div
                      className="mt-1 flex items-center justify-between font-mono text-[10px] tabular-nums"
                      style={{ color: 'var(--text-muted)' }}
                    >
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
                const isShort = tr.direction === 'short' || tr.direction === 'down';
                const dir = isShort ? t('review.dir.short') : t('review.dir.long');
                const marginUsd =
                  tr.margin > 0
                    ? tr.margin
                    : tr.entry_price > 0 && tr.qty > 0
                      ? (tr.entry_price * tr.qty) / Math.max(1, tr.leverage)
                      : 0;
                const roePct = marginUsd > 0 ? (tr.net_pnl / marginUsd) * 100 : null;
                return (
                  <button
                    key={subjKey}
                    type="button"
                    tabIndex={selectorOpen ? 0 : -1}
                    onClick={() => handleSelectSubject(subj)}
                    className="kairos-list-item kairos-row w-full rounded-[6px] border px-2.5 py-2 text-left"
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
                      <span
                        className="font-mono text-xs font-semibold tabular-nums"
                        style={{ color: 'var(--text-primary)' }}
                      >
                        {cleanSymbol(tr.symbol)}
                      </span>
                      <span
                        className="rounded px-1.5 py-0.5 font-mono text-[10px] font-medium tabular-nums"
                        style={{
                          backgroundColor: isShort
                            ? 'var(--candle-bear-soft)'
                            : 'var(--candle-bull-soft)',
                          color: isShort ? 'var(--pnl-neg)' : 'var(--pnl-pos)',
                        }}
                      >
                        {dir} {fmtLev(tr.leverage)}
                      </span>
                    </div>
                    <div
                      className="mt-1 flex items-center justify-between font-mono text-[10px] tabular-nums"
                      style={{ color: 'var(--text-secondary)' }}
                    >
                      <span>
                        {fmtCompactQty(tr.qty)} {baseCoin(tr.symbol)}
                      </span>
                      <span style={{ color: tr.net_pnl >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)' }}>
                        {fmtUsd(tr.net_pnl, true)}
                        {roePct != null ? ` (${fmtPctPoints(roePct, 1)})` : ''}
                      </span>
                    </div>
                    <div
                      className="mt-0.5 flex items-center justify-between font-mono text-[10px] tabular-nums"
                      style={{ color: 'var(--text-muted)' }}
                    >
                      <span>{fmtDateTimeSec(Math.floor((tr.exit_time_ms || tr.entry_time_ms) / 1000))}</span>
                      <span>
                        {tr.status === 'open' ? t('review.card.open') : t('review.card.closed')}
                      </span>
                    </div>
                  </button>
                );
              }
              const pos = subj.position;
              const isShort = pos.qty < 0;
              const dir = isShort ? t('review.dir.short') : t('review.dir.long');
              const posVal =
                pos.position_value && pos.position_value > 0
                  ? pos.position_value
                  : Math.abs(pos.qty * pos.entry_price);
              const posMargin =
                pos.isolated_wallet > 0
                  ? pos.isolated_wallet
                  : posVal / Math.max(1, pos.leverage);
              const posRoe = posMargin > 0 ? (pos.unrealized_pnl / posMargin) * 100 : null;
              return (
                <button
                  key={subjKey}
                  type="button"
                  tabIndex={selectorOpen ? 0 : -1}
                  onClick={() => handleSelectSubject(subj)}
                  className="kairos-list-item kairos-row w-full rounded-[6px] border px-2.5 py-2 text-left"
                  style={{
                    backgroundColor: active
                      ? 'var(--bg-elevated)'
                      : 'var(--bg-canvas)',
                    borderColor: active
                      ? 'var(--accent-info)'
                      : 'var(--border-hairline)',
                  }}
                >
                  <div className="flex items-center justify-between gap-1">
                    <span
                      className="font-mono text-xs font-semibold tabular-nums"
                      style={{ color: 'var(--text-primary)' }}
                    >
                      {cleanSymbol(pos.symbol)}
                    </span>
                    <span
                      className="rounded px-1.5 py-0.5 font-mono text-[10px] font-medium tabular-nums"
                      style={{
                        backgroundColor: isShort
                          ? 'var(--candle-bear-soft)'
                          : 'var(--candle-bull-soft)',
                        color: isShort ? 'var(--pnl-neg)' : 'var(--pnl-pos)',
                      }}
                    >
                      {dir} {fmtLev(pos.leverage)}
                    </span>
                  </div>
                  <div
                    className="mt-1 flex items-center justify-between font-mono text-[10px] tabular-nums"
                    style={{ color: 'var(--text-secondary)' }}
                  >
                    <span>
                      {fmtCompactQty(pos.qty)} {baseCoin(pos.symbol)} · {fmtUsd(posVal)}
                    </span>
                    <span
                      style={{
                        color: pos.unrealized_pnl >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)',
                      }}
                    >
                      {fmtUsd(pos.unrealized_pnl, true)}
                      {posRoe != null ? ` (${fmtPctPoints(posRoe, 1)})` : ''}
                    </span>
                  </div>
                  <div
                    className="mt-0.5 flex items-center justify-between font-mono text-[10px] tabular-nums"
                    style={{ color: 'var(--text-muted)' }}
                  >
                    <span>{t('review.card.avgEntry', { price: fmtPrice(pos.entry_price) })}</span>
                    <span style={{ color: 'var(--accent-info)' }}>
                      {t('review.detail.openPosition')}
                    </span>
                  </div>
                </button>
              );
            })
          )}
        </div>
      </aside>

      {/* Main Full-Bleed Replay Chart */}
      <section className="flex min-w-0 flex-1 flex-col">
        {/* Bybit Trade / Live Position Detail Bar */}
        {(activePosition || activeTrade) && subjectMatchesSymbol ? (
          <div
            className="shrink-0 border-b px-3 py-1.5 text-[11px]"
            style={{
              backgroundColor: 'var(--bg-surface)',
              borderColor: 'var(--border-hairline)',
            }}
          >
            {activePosition ? (
              (() => {
                const pos = activePosition;
                const isShort = pos.qty < 0;
                const dirLabel = isShort ? t('review.dir.short') : t('review.dir.long');
                const notional =
                  pos.position_value && pos.position_value > 0
                    ? pos.position_value
                    : Math.abs(pos.qty * pos.entry_price);
                const margin =
                  pos.isolated_wallet > 0
                    ? pos.isolated_wallet
                    : notional / Math.max(1, pos.leverage);
                const roe = margin > 0 ? (pos.unrealized_pnl / margin) * 100 : null;
                const mmLabel =
                  pos.margin_mode === 'isolated'
                    ? t('review.detail.marginIsolated')
                    : t('review.detail.marginCross');
                const entrySec = activePositionOpenTrade?.entry_time_ms
                  ? Math.floor(activePositionOpenTrade.entry_time_ms / 1000)
                  : 0;
                const holdSec =
                  entrySec > 0 ? Math.max(0, Math.floor(Date.now() / 1000) - entrySec) : null;
                return (
                  <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span
                        className="rounded px-1.5 py-0.5 font-mono text-[10px] font-semibold"
                        style={{
                          backgroundColor: isShort
                            ? 'var(--candle-bear-soft)'
                            : 'var(--candle-bull-soft)',
                          color: isShort ? 'var(--pnl-neg)' : 'var(--pnl-pos)',
                        }}
                      >
                        {dirLabel} {fmtLev(pos.leverage)}
                      </span>
                      <span
                        className="rounded border px-1.5 py-0.5 text-[10px]"
                        style={{
                          borderColor: 'var(--border-hairline)',
                          color: 'var(--text-secondary)',
                        }}
                      >
                        {mmLabel}
                      </span>
                      <span
                        className="rounded px-1.5 py-0.5 text-[10px] font-medium"
                        style={{
                          backgroundColor: 'var(--accent-info-soft, var(--bg-elevated))',
                          color: 'var(--accent-info)',
                        }}
                      >
                        {t('review.detail.openPosition')}
                      </span>
                      {detailExpanded ? (
                        <div className="ml-1 flex flex-wrap items-center gap-x-3.5 gap-y-1 font-mono tabular-nums">
                          <DetailDatum
                            label={t('review.detail.qty')}
                            value={`${fmtCompactQty(pos.qty)} ${baseCoin(pos.symbol)}`}
                          />
                          <DetailDatum
                            label={t('review.detail.notional')}
                            value={fmtUsd(notional)}
                          />
                          <DetailDatum
                            label={t('review.detail.margin')}
                            value={fmtUsd(margin)}
                          />
                          <DetailDatum
                            label={t('review.detail.entry')}
                            value={fmtPrice(pos.entry_price)}
                          />
                          {pos.mark_price > 0 ? (
                            <DetailDatum
                              label={t('review.detail.mark')}
                              value={fmtPrice(pos.mark_price)}
                            />
                          ) : null}
                          {pos.take_profit != null && pos.take_profit > 0 ? (
                            <DetailDatum
                              label={t('review.detail.tp')}
                              value={fmtPrice(pos.take_profit)}
                              color="var(--pnl-pos)"
                            />
                          ) : null}
                          {pos.stop_loss != null && pos.stop_loss > 0 ? (
                            <DetailDatum
                              label={t('review.detail.sl')}
                              value={fmtPrice(pos.stop_loss)}
                              color="var(--pnl-neg)"
                            />
                          ) : null}
                          {pos.liq_price > 0 ? (
                            <DetailDatum
                              label={t('review.detail.liq')}
                              value={fmtPrice(pos.liq_price)}
                              color="var(--state-warn)"
                            />
                          ) : null}
                          {pos.break_even_price != null && pos.break_even_price > 0 ? (
                            <DetailDatum
                              label={t('review.detail.breakEven')}
                              value={fmtPrice(pos.break_even_price)}
                            />
                          ) : null}
                          <DetailDatum
                            label={t('review.detail.unrealizedPnl')}
                            value={`${fmtUsd(pos.unrealized_pnl, true)}${roe != null ? ` (${fmtPctPoints(roe, 1)})` : ''}`}
                            color={pos.unrealized_pnl >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)'}
                            bold
                          />
                          {holdSec != null ? (
                            <DetailDatum
                              label={t('review.detail.holdTime')}
                              value={fmtDurationSec(holdSec)}
                            />
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                    <button
                      type="button"
                      onClick={() => setDetailExpanded((v) => !v)}
                      title={detailExpanded ? t('review.detail.toggleHide') : t('review.detail.toggleShow')}
                      aria-label={detailExpanded ? t('review.detail.toggleHide') : t('review.detail.toggleShow')}
                      className="kairos-btn rounded p-0.5"
                      style={{ color: 'var(--text-muted)' }}
                    >
                      {detailExpanded ? (
                        <ChevronUp className="h-3.5 w-3.5" />
                      ) : (
                        <ChevronDown className="h-3.5 w-3.5" />
                      )}
                    </button>
                  </div>
                );
              })()
            ) : activeTrade ? (
              (() => {
                const tr = activeTrade;
                const isShort = tr.direction === 'short' || tr.direction === 'down';
                const dirLabel = isShort ? t('review.dir.short') : t('review.dir.long');
                const notional = Math.abs(tr.entry_price * tr.qty);
                const margin =
                  tr.margin > 0 ? tr.margin : notional / Math.max(1, tr.leverage);
                const roe = margin > 0 ? (tr.net_pnl / margin) * 100 : null;
                const mmLabel =
                  tr.margin_mode === 'isolated'
                    ? t('review.detail.marginIsolated')
                    : t('review.detail.marginCross');
                const holdSec =
                  tr.exit_time_ms > tr.entry_time_ms && tr.entry_time_ms > 0
                    ? Math.floor((tr.exit_time_ms - tr.entry_time_ms) / 1000)
                    : null;
                const fillCount = tr.fills?.length ?? 0;
                return (
                  <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
                    <div className="flex flex-wrap items-center gap-1.5">
                      <span
                        className="rounded px-1.5 py-0.5 font-mono text-[10px] font-semibold"
                        style={{
                          backgroundColor: isShort
                            ? 'var(--candle-bear-soft)'
                            : 'var(--candle-bull-soft)',
                          color: isShort ? 'var(--pnl-neg)' : 'var(--pnl-pos)',
                        }}
                      >
                        {dirLabel} {fmtLev(tr.leverage)}
                      </span>
                      <span
                        className="rounded border px-1.5 py-0.5 text-[10px]"
                        style={{
                          borderColor: 'var(--border-hairline)',
                          color: 'var(--text-secondary)',
                        }}
                      >
                        {mmLabel}
                      </span>
                      <span
                        className="rounded px-1.5 py-0.5 text-[10px]"
                        style={{
                          backgroundColor: 'var(--bg-elevated)',
                          color: 'var(--text-secondary)',
                        }}
                      >
                        {tr.status === 'open'
                          ? t('review.detail.openTrade')
                          : t('review.detail.closedTrade')}
                      </span>
                      {detailExpanded ? (
                        <div className="ml-1 flex flex-wrap items-center gap-x-3.5 gap-y-1 font-mono tabular-nums">
                          <DetailDatum
                            label={t('review.detail.qty')}
                            value={`${fmtCompactQty(tr.qty)} ${baseCoin(tr.symbol)}`}
                          />
                          <DetailDatum
                            label={t('review.detail.notional')}
                            value={fmtUsd(notional)}
                          />
                          <DetailDatum
                            label={t('review.detail.margin')}
                            value={fmtUsd(margin)}
                          />
                          <DetailDatum
                            label={t('review.detail.entry')}
                            value={fmtPrice(tr.entry_price)}
                          />
                          {tr.exit_price > 0 ? (
                            <DetailDatum
                              label={t('review.detail.exit')}
                              value={fmtPrice(tr.exit_price)}
                            />
                          ) : null}
                          <DetailDatum
                            label={t('review.detail.grossPnl')}
                            value={fmtUsd(tr.realized_pnl, true)}
                            color={
                              tr.realized_pnl > 0
                                ? 'var(--pnl-pos)'
                                : tr.realized_pnl < 0
                                  ? 'var(--pnl-neg)'
                                  : undefined
                            }
                          />
                          <DetailDatum
                            label={t('review.detail.fee')}
                            value={fmtUsd(tr.fee_usd === 0 ? 0 : -tr.fee_usd, true)}
                          />
                          {Math.abs(tr.funding_usd) > 1e-6 ? (
                            <DetailDatum
                              label={t('review.detail.funding')}
                              value={fmtUsd(tr.funding_usd, true)}
                              color={tr.funding_usd >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)'}
                            />
                          ) : null}
                          <DetailDatum
                            label={t('review.detail.netPnl')}
                            value={`${fmtUsd(tr.net_pnl, true)}${roe != null ? ` (${fmtPctPoints(roe, 1)})` : ''}`}
                            color={tr.net_pnl >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)'}
                            bold
                          />
                          {holdSec != null ? (
                            <DetailDatum
                              label={t('review.detail.holdTime')}
                              value={fmtDurationSec(holdSec)}
                            />
                          ) : null}
                          {fillCount > 0 ? (
                            <DetailDatum
                              label={t('review.detail.fills')}
                              value={t('review.detail.fillsCount', { count: fillCount })}
                            />
                          ) : null}
                        </div>
                      ) : null}
                    </div>
                    <button
                      type="button"
                      onClick={() => setDetailExpanded((v) => !v)}
                      title={detailExpanded ? t('review.detail.toggleHide') : t('review.detail.toggleShow')}
                      aria-label={detailExpanded ? t('review.detail.toggleHide') : t('review.detail.toggleShow')}
                      className="kairos-btn rounded p-0.5"
                      style={{ color: 'var(--text-muted)' }}
                    >
                      {detailExpanded ? (
                        <ChevronUp className="h-3.5 w-3.5" />
                      ) : (
                        <ChevronDown className="h-3.5 w-3.5" />
                      )}
                    </button>
                  </div>
                );
              })()
            ) : null}
          </div>
        ) : null}

        {hideFuture ? (
          <div
            className="kairos-animate-view flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-1.5 text-[11px]"
            style={{
              backgroundColor: 'var(--bg-surface)',
              borderColor: 'var(--border-hairline)',
            }}
          >
            <span className="flex items-center gap-1.5 font-mono tabular-nums" style={{ color: 'var(--accent-brand)' }}>
              {playing ? (
                <span className="relative flex h-2 w-2">
                  <span
                    className="kairos-pulse-ring absolute inline-flex h-full w-full rounded-full"
                    style={{ backgroundColor: 'var(--accent-brand)' }}
                  />
                  <span
                    className="relative inline-flex h-2 w-2 rounded-full"
                    style={{ backgroundColor: 'var(--accent-brand)' }}
                  />
                </span>
              ) : null}
              {fmtDateTimeWithSec(cursorTs)}
            </span>
            <input
              type="range"
              aria-label={t('review.replay.cursorAria')}
              min={minCursorTs}
              max={Math.max(minCursorTs + 60, maxCursorTs)}
              step={Math.min(60, tfStepSec)}
              value={Math.min(Math.max(cursorTs, minCursorTs), Math.max(minCursorTs + 60, maxCursorTs))}
              onChange={(e) => {
                setPlaying(false);
                setCursorTs(Number(e.target.value));
              }}
              className="kairos-range h-4 min-w-[120px] flex-1"
            />
            <button
              type="button"
              title={t('review.replay.prevAnomaly')}
              onClick={() => jumpAnomaly(-1)}
              className="kairos-btn rounded border border-transparent p-1"
              style={{ color: 'var(--text-secondary)' }}
            >
              <SkipBack className="h-3 w-3" />
            </button>
            <button
              type="button"
              title={t('review.replay.nextAnomaly')}
              onClick={() => jumpAnomaly(1)}
              className="kairos-btn rounded border border-transparent p-1"
              style={{ color: 'var(--text-secondary)' }}
            >
              <SkipForward className="h-3 w-3" />
            </button>
            {selectedSubject ? (
              <button
                type="button"
                title={t('review.replay.jumpAnchorTitle')}
                onClick={() => {
                  setPlaying(false);
                  setCursorTs(anchorTimestampFor(selectedSubject));
                }}
                className="kairos-btn flex items-center gap-1 rounded-[5px] border px-2 py-0.5 font-medium"
                style={{
                  backgroundColor: 'var(--bg-canvas)',
                  borderColor: 'var(--border-hairline)',
                  color: 'var(--text-secondary)',
                }}
              >
                <Anchor className="h-3 w-3" />
                <span>{t('review.replay.jumpAnchor')}</span>
              </button>
            ) : null}
          </div>
        ) : null}

        <KairosChart
          chartId="review-replay"
          symbol={symbol}
          timeframe={activeTf}
          availableTimeframes={REVIEW_TIMEFRAMES}
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
                : activePosition
                  ? activePosition.qty < 0
                    ? 'down'
                    : 'up'
                  : undefined
          }
          priceLines={reviewPriceLines}
          tradeSegment={reviewTradeSegment}
          visibleRange={reviewVisibleRange}
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

function DetailDatum({
  label,
  value,
  color,
  bold,
}: {
  label: string;
  value: string;
  color?: string;
  bold?: boolean;
}) {
  return (
    <span className="inline-flex items-baseline gap-1 whitespace-nowrap">
      <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
        {label}
      </span>
      <span
        className={bold ? 'font-semibold' : 'font-medium'}
        style={{ color: color ?? 'var(--text-primary)' }}
      >
        {value}
      </span>
    </span>
  );
}
