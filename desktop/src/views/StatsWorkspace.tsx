import { useCallback, useEffect, useMemo, useState } from 'react';
import { AlertTriangle, ArrowUpRight, RefreshCw } from 'lucide-react';
import EquityCurve from '../chart/EquityCurve';
import { StatsSkeleton } from '../components/Skeleton';
import { useI18n } from '../i18n';
import type { ReviewSubjectType, StatsPayload, StatsWorkspaceProps } from '../types/kairos';
import { formatExchangeLabel, resolveAccountExchange } from '../utils/doctrine';
import { fmtDateTimeSec, fmtR, fmtRatioPct, fmtUsd } from '../utils/format';

interface RecentReviewRow {
  key: string;
  subjectKind: ReviewSubjectType;
  subjectId: string;
  symbol: string;
  ts: number;
  ticketId?: string;
  badge: string;
  badgeTone: 'pos' | 'neg' | 'warn';
  metricText: string;
  metricPositive: boolean;
}

function cleanSymbol(sym: string): string {
  return sym.replace(/:USDT$/, '');
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

  const recentRecords = useMemo<RecentReviewRow[]>(() => {
    const list: RecentReviewRow[] = [];
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
      const rawSec = Math.floor(tr.entry_time_ms / 1000);
      const ts = Number.isFinite(rawSec) && rawSec > 0 ? rawSec : 0;
      const isShort = tr.direction === 'short' || tr.direction === 'down';
      const dir = isShort ? t('review.dir.short') : t('review.dir.long');
      list.push({
        key: `trade:${tr.id}`,
        subjectKind: 'trade',
        subjectId: tr.id,
        symbol: tr.symbol,
        ts,
        badge: `${dir} · ${t('review.card.unplanned')}`,
        badgeTone: 'warn',
        metricText: fmtUsd(tr.net_pnl, true),
        metricPositive: tr.net_pnl >= 0,
      });
    }
    list.sort((a, b) => b.ts - a.ts);
    return list.slice(0, 8);
  }, [bootstrap.tickets, bootstrap.unplanned_trades, t]);

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
    summary,
    wallet_reconciliation: recon,
    r_distribution: rDist,
    gate_compliance: compliance,
  } = stats;

  const rBuckets = rDist?.buckets ?? [];
  const executedCount = summary.accepted.n;
  const skippedCount = summary.rejected.n + summary.missed.n;

  return (
    <div
      className="h-full w-full overflow-y-auto"
      style={{ backgroundColor: 'var(--bg-canvas)' }}
    >
      <div className="mx-auto max-w-5xl space-y-4 p-3 sm:space-y-5 sm:p-6">
        {/* Header */}
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div>
            <h1
              className="font-editorial text-xl font-medium"
              style={{ color: 'var(--text-primary)' }}
            >
              {t('stats.title')}
            </h1>
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

        {/* 4 Metric Cards */}
        <div className="grid grid-cols-1 gap-2.5 min-[420px]:grid-cols-2 sm:gap-3 lg:grid-cols-4">
          <MetricCard
            label={t('stats.metric.netPnl')}
            value={fmtUsd(recon.net_pnl_usd, true)}
            sub={t('stats.metric.netPnlSub', {
              bankroll: fmtUsd(recon.trading_bankroll_usd),
              fee: fmtUsd(recon.fee_usd === 0 ? 0 : -recon.fee_usd, true),
            })}
            tone={recon.net_pnl_usd >= 0 ? 'pos' : 'neg'}
          />
          <MetricCard
            label={t('stats.metric.executedSkipped')}
            value={`${executedCount} / ${skippedCount}`}
            sub={t('stats.metric.executedSkippedSub', {
              waiting: summary.waiting.n,
              unplanned: compliance.unplanned_trade_count,
            })}
            tone="neutral"
          />
          <MetricCard
            label={t('stats.metric.meanR')}
            value={fmtR(rDist.mean_r)}
            sub={t('stats.metric.meanRSub', {
              winRate: fmtRatioPct(rDist.win_rate),
              pf: rDist.profit_factor.toFixed(2),
            })}
            tone={rDist.mean_r >= 0 ? 'pos' : 'neg'}
          />
          <MetricCard
            label={t('stats.metric.compliance')}
            value={`${compliance.compliance_score.toFixed(0)}%`}
            sub={t('stats.metric.complianceSub', {
              overrides: compliance.overrides,
              noStop: rDist.without_stop,
            })}
            tone={compliance.compliance_score >= 80 ? 'pos' : 'warn'}
          />
        </div>

        {/* Equity Curve */}
        <EquityCurve
          points={stats.equity_curve ?? []}
          height={240}
          walletSummary={t('stats.walletSummary', {
            exchange: exchangeLabel,
            balance: fmtUsd(recon.wallet_balance_usd),
            unrealized: fmtUsd(recon.unrealized_pnl_usd, true),
          })}
        />

        {/* R Distribution + Recent Drill-Down Grid */}
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-5">
          {/* R Distribution */}
          <div className="kairos-card kairos-contain-panel rounded-[8px] p-3.5 sm:p-4 lg:col-span-3">
            <div className="mb-3 flex items-center justify-between gap-2">
              <h2
                className="font-editorial text-base font-medium"
                style={{ color: 'var(--text-primary)' }}
              >
                {t('stats.rDist.title')}
              </h2>
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
                className="py-10 text-center text-xs"
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
                        : 'var(--state-warn-soft)';
                  const badgeColor =
                    row.badgeTone === 'pos'
                      ? 'var(--pnl-pos)'
                      : row.badgeTone === 'neg'
                        ? 'var(--pnl-neg)'
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
                            className="rounded px-1.5 py-0.5 text-[10px] font-medium"
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
