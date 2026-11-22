import { useCallback, useEffect, useMemo, useState } from 'react';
import { AlertTriangle, ArrowUpRight, RefreshCw } from 'lucide-react';
import EquityCurve from '../chart/EquityCurve';
import { StatsSkeleton } from '../components/Skeleton';
import { useI18n } from '../i18n';
import type {
  PositionSnapshot,
  ReviewSubjectType,
  StatsPayload,
  StatsWorkspaceProps,
  SymbolStatsRow,
} from '../types/kairos';
import { formatExchangeLabel, resolveAccountExchange, sameSymbol } from '../utils/doctrine';
import {
  fmtDateTimeSec,
  fmtPctPoints,
  fmtPrice,
  fmtR,
  fmtRatioPct,
  fmtUsd,
} from '../utils/format';

interface RecentReviewRow {
  key: string;
  subjectKind: ReviewSubjectType;
  subjectId: string;
  symbol: string;
  ts: number;
  ticketId?: string;
  badge: string;
  badgeTone: 'pos' | 'neg' | 'warn' | 'info';
  metricText: string;
  metricPositive: boolean;
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
  if (lev == null || !Number.isFinite(lev) || lev <= 0) return '—';
  return `${Number(lev.toFixed(1))}x`;
}

export function StatsWorkspace({
  client,
  bootstrap,
  onDrillDownReview,
}: StatsWorkspaceProps) {
  const { t, locale } = useI18n();
  const activeExchange = resolveAccountExchange(bootstrap.status);
  const exchangeLabel = formatExchangeLabel(activeExchange, locale);
  const [stats, setStats] = useState<StatsPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [errorState, setErrorState] = useState<{ raw: string | null } | null>(null);
  const [hoveredBucket, setHoveredBucket] = useState<string | null>(null);

  const fetchStats = useCallback(async () => {
    setLoading(true);
    setErrorState(null);
    try {
      const res = await client.getStats();
      setStats(res);
    } catch (err) {
      setErrorState({ raw: err instanceof Error ? err.message : null });
    } finally {
      setLoading(false);
    }
  }, [client]);

  const error = errorState
    ? errorState.raw !== null
      ? errorState.raw
      : t('stats.loadFailed')
    : null;

  useEffect(() => {
    void fetchStats();
  }, [fetchStats]);

  const maxBucketCount = useMemo(() => {
    if (!stats?.r_distribution?.buckets?.length) return 1;
    return Math.max(1, ...stats.r_distribution.buckets.map((b) => b.count));
  }, [stats?.r_distribution?.buckets]);

  const totalBucketTrades = useMemo(() => {
    if (!stats?.r_distribution?.buckets?.length) return 0;
    return stats.r_distribution.buckets.reduce((sum, b) => sum + b.count, 0);
  }, [stats?.r_distribution?.buckets]);

  const openPositions = useMemo(
    () => bootstrap.open_positions ?? [],
    [bootstrap.open_positions],
  );

  const openTradeTimeByKey = useMemo(() => {
    const map = new Map<string, number>();
    for (const tr of bootstrap.unplanned_trades ?? []) {
      if (tr.status === 'open' && tr.entry_time_ms > 0) {
        map.set(`${tr.symbol}|${tr.position_side}`, Math.floor(tr.entry_time_ms / 1000));
      }
    }
    return map;
  }, [bootstrap.unplanned_trades]);

  const recentRecords = useMemo<RecentReviewRow[]>(() => {
    const list: RecentReviewRow[] = [];
    const openPosKeys = new Set<string>();
    for (const pos of openPositions) {
      openPosKeys.add(`${pos.symbol}|${pos.position_side}`);
      const entrySec = openTradeTimeByKey.get(`${pos.symbol}|${pos.position_side}`) ?? pos.updated_at;
      const isShort = pos.qty < 0;
      const dir = isShort ? t('review.dir.short') : t('review.dir.long');
      list.push({
        key: `position:${pos.symbol}:${pos.position_side}`,
        subjectKind: 'position',
        subjectId: `${pos.symbol}:${pos.position_side}`,
        symbol: pos.symbol,
        ts: entrySec || 0,
        ticketId: pos.linked_ticket_id,
        badge: `${dir} · ${t('stats.recent.positionBadge', { lev: Number(pos.leverage.toFixed(1)) })}`,
        badgeTone: 'info',
        metricText: fmtUsd(pos.unrealized_pnl, true),
        metricPositive: pos.unrealized_pnl >= 0,
      });
    }
    for (const b of bootstrap.tickets ?? []) {
      const rawTs = b.ticket.entry_triggered_at || b.ticket.signal_at || b.ticket.created_at;
      const ts = Number.isFinite(rawTs) && rawTs > 0 ? rawTs : 0;
      const isShort = b.ticket.direction === 'down';
      const dir = isShort ? t('review.dir.short') : t('review.dir.long');
      const netR = b.outcome?.net_r;
      const netPnl = b.linked_trade?.net_pnl;
      list.push({
        key: `ticket:${b.ticket.id}`,
        subjectKind: 'ticket',
        subjectId: b.ticket.id,
        symbol: b.ticket.symbol,
        ts,
        ticketId: b.ticket.id,
        badge: `${dir} · ${b.ticket.grade}`,
        badgeTone: isShort ? 'neg' : 'pos',
        metricText:
          netR != null
            ? fmtR(netR)
            : netPnl != null
              ? fmtUsd(netPnl, true)
              : b.ticket.status,
        metricPositive: (netR ?? netPnl ?? 0) >= 0,
      });
    }
    for (const tr of bootstrap.unplanned_trades ?? []) {
      if (tr.status === 'open' && openPosKeys.has(`${tr.symbol}|${tr.position_side}`)) {
        continue;
      }
      const rawSec = Math.floor((tr.exit_time_ms || tr.entry_time_ms) / 1000);
      const ts = Number.isFinite(rawSec) && rawSec > 0 ? rawSec : 0;
      const isShort = tr.direction === 'short' || tr.direction === 'down';
      const dir = isShort ? t('review.dir.short') : t('review.dir.long');
      list.push({
        key: `trade:${tr.id}`,
        subjectKind: 'trade',
        subjectId: tr.id,
        symbol: tr.symbol,
        ts,
        badge: t('stats.recent.tradeBadge', {
          dir,
          lev: Number((tr.leverage || 1).toFixed(1)),
        }),
        badgeTone: isShort ? 'neg' : 'pos',
        metricText: fmtUsd(tr.net_pnl, true),
        metricPositive: tr.net_pnl >= 0,
      });
    }
    list.sort((a, b) => {
      if (a.subjectKind === 'position' && b.subjectKind !== 'position') return -1;
      if (b.subjectKind === 'position' && a.subjectKind !== 'position') return 1;
      return b.ts - a.ts;
    });
    return list.slice(0, 10);
  }, [openPositions, openTradeTimeByKey, bootstrap.tickets, bootstrap.unplanned_trades, t]);

  const handleDrillPosition = useCallback(
    (pos: PositionSnapshot) => {
      const entrySec = openTradeTimeByKey.get(`${pos.symbol}|${pos.position_side}`) ?? pos.updated_at;
      onDrillDownReview(
        pos.symbol,
        entrySec > 0 ? entrySec : undefined,
        pos.linked_ticket_id,
        'position',
        `${pos.symbol}:${pos.position_side}`,
      );
    },
    [onDrillDownReview, openTradeTimeByKey],
  );

  const handleDrillSymbolRow = useCallback(
    (row: SymbolStatsRow) => {
      const openPos = openPositions.find((p) => sameSymbol(p.symbol, row.symbol));
      if (openPos && !row.sample_trade_id) {
        handleDrillPosition(openPos);
        return;
      }
      if (row.sample_trade_id) {
        onDrillDownReview(
          row.symbol,
          row.last_time_ms > 0 ? Math.floor(row.last_time_ms / 1000) : undefined,
          undefined,
          'trade',
          row.sample_trade_id,
        );
        return;
      }
      if (openPos) {
        handleDrillPosition(openPos);
        return;
      }
      onDrillDownReview(row.symbol);
    },
    [openPositions, handleDrillPosition, onDrillDownReview],
  );

  if (loading && !stats) {
    return <StatsSkeleton />;
  }

  if (error && !stats) {
    return (
      <div
        className="flex h-full w-full flex-col items-center justify-center gap-3 p-6 text-xs"
        style={{ backgroundColor: 'var(--bg-canvas)', color: 'var(--text-secondary)' }}
      >
        <AlertTriangle className="h-5 w-5" style={{ color: 'var(--candle-bear)' }} />
        <span>{error}</span>
        <button
          type="button"
          onClick={() => void fetchStats()}
          className="kairos-btn rounded-[6px] border px-3 py-1.5 font-medium"
          style={{
            backgroundColor: 'var(--bg-surface)',
            borderColor: 'var(--border-hairline)',
            color: 'var(--text-primary)',
          }}
        >
          {t('stats.retry')}
        </button>
      </div>
    );
  }

  if (!stats) return null;

  const {
    wallet_reconciliation: recon,
    r_distribution: rDist,
    gate_compliance: compliance,
  } = stats;

  const bySymbol = stats.by_symbol ?? [];
  const rBuckets = rDist?.buckets ?? [];
  const closedCount = recon.closed_trades_count ?? rDist.total_trades;
  const openCount = recon.open_positions_count ?? openPositions.length;
  const winCount = recon.win_count ?? Math.round(rDist.win_rate * closedCount);
  const lossCount = recon.loss_count ?? Math.max(0, closedCount - winCount);
  const equityVal =
    recon.total_equity_usd && recon.total_equity_usd > 0
      ? recon.total_equity_usd
      : recon.wallet_balance_usd;
  const pfDisplay =
    rDist.profit_factor >= 99 ? '∞' : rDist.profit_factor.toFixed(2);
  const acctModeBadge = [
    recon.account_type === 'UNIFIED' ? 'UTA' : recon.account_type || '',
    recon.margin_mode === 'isolated'
      ? t('review.detail.marginIsolated')
      : recon.margin_mode === 'cross'
        ? t('review.detail.marginCross')
        : '',
  ]
    .filter(Boolean)
    .join(' · ');

  return (
    <div
      className="h-full w-full overflow-y-auto"
      style={{ backgroundColor: 'var(--bg-canvas)' }}
    >
      <div className="mx-auto max-w-5xl space-y-4 p-3 sm:space-y-5 sm:p-6">
        {/* Header */}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h1
                className="font-editorial text-xl font-medium"
                style={{ color: 'var(--text-primary)' }}
              >
                {t('stats.title')}
              </h1>
              {acctModeBadge ? (
                <span
                  className="rounded border px-1.5 py-0.5 font-mono text-[10px] font-medium"
                  style={{
                    backgroundColor: 'var(--bg-surface)',
                    borderColor: 'var(--border-hairline)',
                    color: 'var(--text-secondary)',
                  }}
                >
                  {exchangeLabel} · {acctModeBadge}
                </span>
              ) : null}
            </div>
            <p className="mt-0.5 text-xs" style={{ color: 'var(--text-secondary)' }}>
              {t('stats.subtitle', { exchange: exchangeLabel })}
            </p>
          </div>
          <button
            type="button"
            onClick={() => void fetchStats()}
            className="kairos-btn flex items-center gap-1.5 rounded-[6px] border px-3 py-1.5 text-xs font-medium"
            style={{
              backgroundColor: 'var(--bg-surface)',
              borderColor: 'var(--border-hairline)',
              color: 'var(--text-primary)',
            }}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? 'animate-spin' : ''}`} />
            <span>{t('stats.refresh')}</span>
          </button>
        </div>

        {/* 4 Bybit-Centric Metric Cards */}
        <div className="grid grid-cols-1 gap-2.5 min-[420px]:grid-cols-2 sm:gap-3 lg:grid-cols-4">
          <MetricCard
            label={t('stats.metric.netPnl')}
            value={fmtUsd(recon.net_pnl_usd, true)}
            sub={t('stats.metric.netPnlSub', {
              gross: fmtUsd(recon.realized_pnl_usd, true),
              fee: fmtUsd(recon.fee_usd === 0 ? 0 : -recon.fee_usd, true),
              funding: fmtUsd(recon.funding_usd, true),
            })}
            tone={recon.net_pnl_usd >= 0 ? 'pos' : 'neg'}
          />
          <MetricCard
            label={t('stats.metric.accountEquity', { exchange: exchangeLabel })}
            value={fmtUsd(equityVal)}
            sub={t('stats.metric.accountEquitySub', {
              avail: fmtUsd(recon.available_balance_usd ?? recon.wallet_balance_usd),
              im: fmtUsd(recon.initial_margin_usd ?? 0),
              upl: fmtUsd(recon.unrealized_pnl_usd, true),
            })}
            tone="neutral"
          />
          <MetricCard
            label={t('stats.metric.winRate')}
            value={
              closedCount > 0
                ? `${fmtRatioPct(rDist.win_rate)} · PF ${pfDisplay}`
                : '—'
            }
            sub={t('stats.metric.winRateSub', {
              wins: winCount,
              losses: lossCount,
              closed: closedCount,
              lev: fmtLev(recon.avg_leverage),
            })}
            tone={
              closedCount === 0
                ? 'neutral'
                : rDist.win_rate >= 0.5 || recon.net_pnl_usd >= 0
                  ? 'pos'
                  : 'neg'
            }
          />
          <MetricCard
            label={t('stats.metric.openRisk')}
            value={
              openCount > 0
                ? `${openCount} · ${fmtUsd(recon.unrealized_pnl_usd, true)}`
                : `${compliance.compliance_score.toFixed(0)}%`
            }
            sub={t('stats.metric.openRiskSub', {
              bankroll: fmtUsd(recon.trading_bankroll_usd),
              upl: fmtUsd(recon.unrealized_pnl_usd, true),
              compliance: `${compliance.compliance_score.toFixed(0)}%`,
            })}
            tone={
              openCount > 0
                ? recon.unrealized_pnl_usd >= 0
                  ? 'pos'
                  : 'neg'
                : compliance.compliance_score >= 80
                  ? 'pos'
                  : 'warn'
            }
          />
        </div>

        {/* Equity Curve */}
        <EquityCurve
          points={stats.equity_curve ?? []}
          height={240}
          walletSummary={t('stats.walletSummary', {
            exchange: exchangeLabel,
            balance: fmtUsd(recon.wallet_balance_usd),
            equity: fmtUsd(equityVal),
            unrealized: fmtUsd(recon.unrealized_pnl_usd, true),
          })}
        />

        {/* Live Open Positions (Bybit Positions Table) */}
        {openPositions.length > 0 ? (
          <div className="kairos-card kairos-contain-panel rounded-[8px] p-3.5 sm:p-4">
            <h2
              className="mb-3 font-editorial text-base font-medium"
              style={{ color: 'var(--text-primary)' }}
            >
              {t('stats.positions.title')}
            </h2>
            <div className="overflow-x-auto">
              <div className="min-w-[600px] space-y-1.5">
                <div
                  className="grid grid-cols-[1.15fr_1fr_1.1fr_1.2fr_1.1fr_0.9fr_1.2fr] gap-2 px-2.5 pb-1 text-[10px] font-medium"
                  style={{ color: 'var(--text-muted)' }}
                >
                  <span>{t('stats.positions.colSymbol')}</span>
                  <span>{t('stats.positions.colSideLev')}</span>
                  <span className="text-right">{t('stats.positions.colSizeVal')}</span>
                  <span className="text-right">{t('stats.positions.colEntryMark')}</span>
                  <span className="text-right">{t('stats.positions.colSlTp')}</span>
                  <span className="text-right">{t('stats.positions.colLiq')}</span>
                  <span className="text-right">{t('stats.positions.colPnl')}</span>
                </div>
                {openPositions.map((pos) => {
                  const isShort = pos.qty < 0;
                  const dir = isShort ? t('review.dir.short') : t('review.dir.long');
                  const mmLabel =
                    pos.margin_mode === 'isolated'
                      ? t('review.detail.marginIsolated')
                      : t('review.detail.marginCross');
                  const posVal =
                    pos.position_value && pos.position_value > 0
                      ? pos.position_value
                      : Math.abs(pos.qty * pos.entry_price);
                  const margin =
                    pos.isolated_wallet > 0
                      ? pos.isolated_wallet
                      : posVal / Math.max(1, pos.leverage);
                  const roe = margin > 0 ? (pos.unrealized_pnl / margin) * 100 : null;
                  return (
                    <button
                      key={`${pos.symbol}:${pos.position_side}`}
                      type="button"
                      onClick={() => handleDrillPosition(pos)}
                      className="kairos-row kairos-press group grid w-full grid-cols-[1.15fr_1fr_1.1fr_1.2fr_1.1fr_0.9fr_1.2fr] items-center gap-2 rounded-[6px] border px-2.5 py-2 text-left font-mono text-[11px] tabular-nums"
                      style={{
                        backgroundColor: 'var(--bg-canvas)',
                        borderColor: 'var(--border-hairline)',
                      }}
                      title={t('stats.recent.openReview')}
                    >
                      <span
                        className="flex items-center gap-1 font-semibold"
                        style={{ color: 'var(--text-primary)' }}
                      >
                        <span className="truncate">{cleanSymbol(pos.symbol)}</span>
                        <ArrowUpRight
                          className="h-3 w-3 shrink-0 opacity-60 transition-transform duration-150 group-hover:translate-x-0.5 group-hover:-translate-y-0.5"
                          style={{ color: 'var(--text-muted)' }}
                        />
                      </span>
                      <span className="flex items-center gap-1">
                        <span
                          className="rounded px-1.5 py-0.5 text-[10px] font-medium"
                          style={{
                            backgroundColor: isShort
                              ? 'var(--candle-bear-soft)'
                              : 'var(--candle-bull-soft)',
                            color: isShort ? 'var(--pnl-neg)' : 'var(--pnl-pos)',
                          }}
                        >
                          {dir} {fmtLev(pos.leverage)}
                        </span>
                        <span className="text-[10px]" style={{ color: 'var(--text-muted)' }}>
                          {mmLabel}
                        </span>
                      </span>
                      <span className="text-right">
                        <span className="block" style={{ color: 'var(--text-primary)' }}>
                          {fmtCompactQty(pos.qty)} {baseCoin(pos.symbol)}
                        </span>
                        <span className="block text-[10px]" style={{ color: 'var(--text-muted)' }}>
                          {fmtUsd(posVal)}
                        </span>
                      </span>
                      <span className="text-right">
                        <span className="block" style={{ color: 'var(--text-primary)' }}>
                          {fmtPrice(pos.entry_price)}
                        </span>
                        <span className="block text-[10px]" style={{ color: 'var(--text-muted)' }}>
                          {fmtPrice(pos.mark_price)}
                        </span>
                      </span>
                      <span className="text-right">
                        <span className="block" style={{ color: 'var(--pnl-pos)' }}>
                          {pos.take_profit && pos.take_profit > 0 ? fmtPrice(pos.take_profit) : '—'}
                        </span>
                        <span className="block text-[10px]" style={{ color: 'var(--pnl-neg)' }}>
                          {pos.stop_loss && pos.stop_loss > 0 ? fmtPrice(pos.stop_loss) : '—'}
                        </span>
                      </span>
                      <span
                        className="text-right"
                        style={{ color: pos.liq_price > 0 ? 'var(--state-warn)' : 'var(--text-muted)' }}
                      >
                        {pos.liq_price > 0 ? fmtPrice(pos.liq_price) : '—'}
                      </span>
                      <span className="text-right">
                        <span
                          className="block font-semibold"
                          style={{
                            color: pos.unrealized_pnl >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)',
                          }}
                        >
                          {fmtUsd(pos.unrealized_pnl, true)}
                        </span>
                        {roe != null ? (
                          <span
                            className="block text-[10px]"
                            style={{
                              color: roe >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)',
                            }}
                          >
                            {fmtPctPoints(roe, 1)}
                          </span>
                        ) : null}
                      </span>
                    </button>
                  );
                })}
              </div>
            </div>
          </div>
        ) : null}

        {/* Per-Symbol Breakdown + Recent Records Grid */}
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-5">
          {/* Performance by Symbol */}
          <div className="kairos-card kairos-contain-panel rounded-[8px] p-3.5 sm:p-4 lg:col-span-3">
            <h2
              className="mb-3 font-editorial text-base font-medium"
              style={{ color: 'var(--text-primary)' }}
            >
              {t('stats.bySymbol.title')}
            </h2>

            {bySymbol.length === 0 ? (
              <div
                className="py-10 text-center text-xs"
                style={{ color: 'var(--text-muted)' }}
              >
                {t('stats.bySymbol.empty')}
              </div>
            ) : (
              <div className="overflow-x-auto">
                <div className="min-w-[440px] space-y-1.5">
                  <div
                    className="grid grid-cols-[1.25fr_0.65fr_0.75fr_0.65fr_0.95fr_0.8fr_1.05fr] gap-2 px-2 pb-1 text-[10px] font-medium"
                    style={{ color: 'var(--text-muted)' }}
                  >
                    <span>{t('stats.bySymbol.colSymbol')}</span>
                    <span className="text-right">{t('stats.bySymbol.colTrades')}</span>
                    <span className="text-right">{t('stats.bySymbol.colWinRate')}</span>
                    <span className="text-right">{t('stats.bySymbol.colLev')}</span>
                    <span className="text-right">{t('stats.bySymbol.colNotional')}</span>
                    <span className="text-right">{t('stats.bySymbol.colFee')}</span>
                    <span className="text-right">{t('stats.bySymbol.colNetPnl')}</span>
                  </div>
                  {bySymbol.map((row) => {
                    const hasClosed = row.closed_count > 0;
                    const primaryPnl = hasClosed ? row.net_pnl_usd : row.unrealized_pnl_usd;
                    return (
                      <button
                        key={row.symbol}
                        type="button"
                        onClick={() => handleDrillSymbolRow(row)}
                        className="kairos-row kairos-press group grid w-full grid-cols-[1.25fr_0.65fr_0.75fr_0.65fr_0.95fr_0.8fr_1.05fr] items-center gap-2 rounded-[6px] border px-2 py-2 text-left font-mono text-[11px] tabular-nums"
                        style={{
                          backgroundColor: 'var(--bg-canvas)',
                          borderColor: 'var(--border-hairline)',
                        }}
                        title={t('stats.recent.openReview')}
                      >
                        <span className="flex min-w-0 items-center gap-1.5">
                          <span
                            className="truncate font-semibold"
                            style={{ color: 'var(--text-primary)' }}
                          >
                            {cleanSymbol(row.symbol)}
                          </span>
                          {row.open_count > 0 ? (
                            <span
                              className="shrink-0 rounded px-1 py-0.5 text-[9px] font-medium"
                              style={{
                                backgroundColor: 'var(--bg-elevated)',
                                color: 'var(--accent-info)',
                              }}
                            >
                              {t('stats.bySymbol.openTag')}
                            </span>
                          ) : null}
                        </span>
                        <span className="text-right" style={{ color: 'var(--text-primary)' }}>
                          {row.closed_count}
                          {row.open_count > 0 ? `+${row.open_count}` : ''}
                        </span>
                        <span className="text-right" style={{ color: 'var(--text-secondary)' }}>
                          {hasClosed ? fmtRatioPct(row.win_rate, 0) : '—'}
                        </span>
                        <span className="text-right" style={{ color: 'var(--text-secondary)' }}>
                          {fmtLev(row.avg_leverage)}
                        </span>
                        <span className="text-right" style={{ color: 'var(--text-secondary)' }}>
                          {fmtUsd(row.total_notional_usd)}
                        </span>
                        <span className="text-right" style={{ color: 'var(--text-muted)' }}>
                          {fmtUsd(row.fee_usd === 0 ? 0 : -row.fee_usd, true)}
                        </span>
                        <span className="text-right">
                          <span
                            className="block font-semibold"
                            style={{
                              color: primaryPnl >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)',
                            }}
                          >
                            {fmtUsd(primaryPnl, true)}
                          </span>
                          {hasClosed && Math.abs(row.unrealized_pnl_usd) > 1e-4 ? (
                            <span
                              className="block text-[9px]"
                              style={{
                                color:
                                  row.unrealized_pnl_usd >= 0
                                    ? 'var(--pnl-pos)'
                                    : 'var(--pnl-neg)',
                              }}
                            >
                              {fmtUsd(row.unrealized_pnl_usd, true)}
                            </span>
                          ) : null}
                        </span>
                      </button>
                    );
                  })}
                </div>
              </div>
            )}
          </div>

          {/* Recent Records Drill-Down to Review */}
          <div className="kairos-card kairos-contain-panel rounded-[8px] p-3.5 sm:p-4 lg:col-span-2">
            <h2
              className="mb-3 font-editorial text-base font-medium"
              style={{ color: 'var(--text-primary)' }}
            >
              {t('stats.recent.title')}
            </h2>

            {recentRecords.length === 0 ? (
              <div
                className="py-10 text-center text-xs"
                style={{ color: 'var(--text-muted)' }}
              >
                {t('stats.recent.empty')}
              </div>
            ) : (
              <div className="space-y-1.5">
                {recentRecords.map((row) => {
                  const badgeBg =
                    row.badgeTone === 'pos'
                      ? 'var(--candle-bull-soft)'
                      : row.badgeTone === 'neg'
                        ? 'var(--candle-bear-soft)'
                        : row.badgeTone === 'info'
                          ? 'var(--bg-elevated)'
                          : 'var(--state-warn-soft)';
                  const badgeColor =
                    row.badgeTone === 'pos'
                      ? 'var(--pnl-pos)'
                      : row.badgeTone === 'neg'
                        ? 'var(--pnl-neg)'
                        : row.badgeTone === 'info'
                          ? 'var(--accent-info)'
                          : 'var(--state-warn)';
                  return (
                    <button
                      key={row.key}
                      type="button"
                      onClick={() =>
                        onDrillDownReview(
                          row.symbol,
                          row.ts > 0 ? row.ts : undefined,
                          row.ticketId,
                          row.subjectKind,
                          row.subjectId,
                        )
                      }
                      className="kairos-row kairos-press group flex w-full items-center justify-between gap-2 rounded-[6px] border px-2.5 py-2 text-left"
                      style={{
                        backgroundColor: 'var(--bg-canvas)',
                        borderColor: 'var(--border-hairline)',
                      }}
                      title={t('stats.recent.openReview')}
                    >
                      <div className="min-w-0">
                        <div className="flex items-center gap-1.5">
                          <span
                            className="font-mono text-xs font-semibold tabular-nums"
                            style={{ color: 'var(--text-primary)' }}
                          >
                            {cleanSymbol(row.symbol)}
                          </span>
                          <span
                            className="rounded px-1.5 py-0.5 font-mono text-[10px] font-medium tabular-nums"
                            style={{ backgroundColor: badgeBg, color: badgeColor }}
                          >
                            {row.badge}
                          </span>
                        </div>
                        <div
                          className="mt-0.5 font-mono text-[10px] tabular-nums"
                          style={{ color: 'var(--text-muted)' }}
                        >
                          {row.ts > 0 ? fmtDateTimeSec(row.ts) : '—'}
                        </div>
                      </div>

                      <div className="flex shrink-0 items-center gap-1.5">
                        <span
                          className="font-mono text-xs font-medium tabular-nums"
                          style={{
                            color: row.metricPositive
                              ? 'var(--pnl-pos)'
                              : 'var(--pnl-neg)',
                          }}
                        >
                          {row.metricText}
                        </span>
                        <ArrowUpRight
                          className="h-3.5 w-3.5 transition-transform duration-150 group-hover:translate-x-0.5 group-hover:-translate-y-0.5"
                          style={{ color: 'var(--text-muted)' }}
                        />
                      </div>
                    </button>
                  );
                })}
              </div>
            )}
          </div>
        </div>

        {/* R-Multiple Distribution */}
        <div className="kairos-card kairos-contain-panel rounded-[8px] p-3.5 sm:p-4">
          <div className="mb-3 flex items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <h2
                className="font-editorial text-base font-medium"
                style={{ color: 'var(--text-primary)' }}
              >
                {t('stats.rDist.title')}
              </h2>
              <span
                className="font-mono text-[11px] tabular-nums"
                style={{ color: 'var(--text-muted)' }}
              >
                {t('stats.metric.meanR')}: {fmtR(rDist.mean_r)}
              </span>
            </div>
            {hoveredBucket && totalBucketTrades > 0 ? (
              <span
                className="font-mono text-[11px] tabular-nums"
                style={{ color: 'var(--text-secondary)' }}
              >
                {(() => {
                  const hit = rBuckets.find((b) => b.label === hoveredBucket);
                  if (!hit) return null;
                  const share = ((hit.count / totalBucketTrades) * 100).toFixed(1);
                  return t('stats.rDist.share', { pct: share });
                })()}
              </span>
            ) : null}
          </div>

          {rBuckets.length === 0 ? (
            <div
              className="py-8 text-center text-xs"
              style={{ color: 'var(--text-muted)' }}
            >
              {t('stats.rDist.empty')}
            </div>
          ) : (
            <div className="space-y-1.5">
              {rBuckets.map((b) => {
                const pct = (b.count / maxBucketCount) * 100;
                const sharePct =
                  totalBucketTrades > 0
                    ? ((b.count / totalBucketTrades) * 100).toFixed(0)
                    : '0';
                const pos = b.min_r >= 0 || b.net_pnl_usd >= 0;
                const isHovered = hoveredBucket === b.label;
                const dimmed = hoveredBucket !== null && !isHovered;
                return (
                  <div
                    key={b.label}
                    tabIndex={0}
                    onMouseEnter={() => setHoveredBucket(b.label)}
                    onMouseLeave={() => setHoveredBucket(null)}
                    onFocus={() => setHoveredBucket(b.label)}
                    onBlur={() => setHoveredBucket(null)}
                    onClick={() =>
                      setHoveredBucket((prev) => (prev === b.label ? null : b.label))
                    }
                    aria-label={`${b.label}: ${t('stats.rDist.tradeCount', { count: b.count })}, ${t('stats.rDist.share', { pct: sharePct })}, ${fmtUsd(b.net_pnl_usd, true)}`}
                    className="grid cursor-default grid-cols-[64px_1fr_64px_64px] items-center gap-1.5 rounded-[5px] px-1.5 py-1 font-mono text-[11px] tabular-nums transition-colors duration-150 sm:grid-cols-[92px_1fr_92px_78px] sm:gap-2"
                    style={{
                      backgroundColor: isHovered ? 'var(--bg-hover)' : 'transparent',
                      opacity: dimmed ? 0.55 : 1,
                    }}
                  >
                    <span
                      className="truncate"
                      style={{
                        color: isHovered ? 'var(--text-primary)' : 'var(--text-secondary)',
                      }}
                    >
                      {b.label}
                    </span>
                    <div
                      className="h-3.5 overflow-hidden rounded-[4px]"
                      style={{ backgroundColor: 'var(--bg-canvas)' }}
                    >
                      <div
                        className="h-full rounded-[4px] transition-[width] duration-300"
                        style={{
                          width: `${Math.max(b.count > 0 ? 4 : 0, pct)}%`,
                          backgroundColor: pos
                            ? 'var(--candle-bull)'
                            : 'var(--candle-bear)',
                        }}
                      />
                    </div>
                    <span
                      className="whitespace-nowrap text-right"
                      style={{ color: 'var(--text-primary)' }}
                    >
                      {t('stats.rDist.tradeCount', { count: b.count })}
                      <span className="ml-1 text-[10px]" style={{ color: 'var(--text-muted)' }}>
                        {sharePct}%
                      </span>
                    </span>
                    <span
                      className="text-right"
                      style={{
                        color:
                          b.net_pnl_usd >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)',
                      }}
                    >
                      {fmtUsd(b.net_pnl_usd, true)}
                    </span>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function MetricCard({
  label,
  value,
  sub,
  tone,
}: {
  label: string;
  value: string;
  sub: string;
  tone: 'pos' | 'neg' | 'warn' | 'neutral';
}) {
  const valueColor =
    tone === 'pos'
      ? 'var(--pnl-pos)'
      : tone === 'neg'
        ? 'var(--pnl-neg)'
        : tone === 'warn'
          ? 'var(--state-warn)'
          : 'var(--text-primary)';

  const topLineColor =
    tone === 'pos'
      ? 'var(--candle-bull)'
      : tone === 'neg'
        ? 'var(--candle-bear)'
        : tone === 'warn'
          ? 'var(--state-warn)'
          : 'var(--border-strong)';

  return (
    <div className="kairos-card kairos-card-interactive relative overflow-hidden rounded-[8px] p-3.5">
      <span
        aria-hidden="true"
        className="absolute top-0 right-0 left-0 h-[2px] opacity-70"
        style={{ backgroundColor: topLineColor }}
      />
      <div className="text-[11px] font-medium" style={{ color: 'var(--text-secondary)' }}>
        {label}
      </div>
      <div
        className="mt-1.5 font-mono text-xl font-semibold tracking-tight tabular-nums"
        style={{ color: valueColor }}
      >
        {value}
      </div>
      <div
        className="mt-1 break-words font-mono text-[10px] leading-snug tabular-nums"
        style={{ color: 'var(--text-muted)' }}
      >
        {sub}
      </div>
    </div>
  );
}
