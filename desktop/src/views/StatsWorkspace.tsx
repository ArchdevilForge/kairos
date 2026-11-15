import { useCallback, useEffect, useMemo, useState } from 'react';
import { AlertTriangle, RefreshCw } from 'lucide-react';
import EquityCurve from '../chart/EquityCurve';
import { useI18n } from '../i18n';
import type { StatsPayload, StatsWorkspaceProps } from '../types/kairos';
import { fmtR, fmtRatioPct, fmtUsd } from '../utils/format';

export function StatsWorkspace({ client }: StatsWorkspaceProps) {
  const { t } = useI18n();
  const [stats, setStats] = useState<StatsPayload | null>(null);
  const [loading, setLoading] = useState(true);
  const [errorState, setErrorState] = useState<{ raw: string | null } | null>(null);

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

  if (loading && !stats) {
    return (
      <div
        className="flex h-full w-full items-center justify-center text-xs"
        style={{ backgroundColor: 'var(--bg-canvas)', color: 'var(--text-secondary)' }}
      >
        <RefreshCw className="mr-2 h-4 w-4 animate-spin" style={{ color: 'var(--accent-brand)' }} />
        <span>{t('stats.loading')}</span>
      </div>
    );
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
          className="rounded border px-3 py-1.5"
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
      <div className="mx-auto max-w-5xl space-y-5 p-6">
        {/* Header */}
        <div className="flex items-center justify-between">
          <div>
            <h1 className="font-editorial text-xl font-medium" style={{ color: 'var(--text-primary)' }}>
              {t('stats.title')}
            </h1>
            <p className="mt-0.5 text-xs" style={{ color: 'var(--text-secondary)' }}>
              {t('stats.subtitle')}
            </p>
          </div>
          <button
            type="button"
            onClick={() => void fetchStats()}
            className="flex items-center gap-1.5 rounded border px-3 py-1.5 text-xs hover:opacity-85"
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

        {/* 4 Clean Metric Cards */}
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
          <MetricCard
            label={t('stats.metric.netPnl')}
            value={fmtUsd(recon.net_pnl_usd, true)}
            sub={t('stats.metric.netPnlSub', {
              bankroll: fmtUsd(recon.trading_bankroll_usd),
              fee: fmtUsd(-Math.abs(recon.fee_usd), true),
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
            balance: fmtUsd(recon.wallet_balance_usd),
            unrealized: fmtUsd(recon.unrealized_pnl_usd, true),
          })}
        />

        {/* R Distribution */}
        <div
          className="rounded border p-4"
          style={{
            backgroundColor: 'var(--bg-surface)',
            borderColor: 'var(--border-hairline)',
          }}
        >
          <h2 className="mb-3 font-editorial text-base font-medium" style={{ color: 'var(--text-primary)' }}>
            {t('stats.rDist.title')}
          </h2>

          {rBuckets.length === 0 ? (
            <div className="py-10 text-center text-xs" style={{ color: 'var(--text-muted)' }}>
              {t('stats.rDist.empty')}
            </div>
          ) : (
            <div className="space-y-2">
              {rBuckets.map((b) => {
                const pct = (b.count / maxBucketCount) * 100;
                const pos = b.min_r >= 0 || b.net_pnl_usd >= 0;
                return (
                  <div
                    key={b.label}
                    className="grid grid-cols-[100px_1fr_76px_80px] items-center gap-2 font-mono text-[11px] tabular-nums"
                  >
                    <span className="truncate" style={{ color: 'var(--text-secondary)' }}>
                      {b.label}
                    </span>
                    <div
                      className="h-3.5 overflow-hidden rounded-sm"
                      style={{ backgroundColor: 'var(--bg-canvas)' }}
                    >
                      <div
                        className="h-full rounded-sm transition-all"
                        style={{
                          width: `${Math.max(b.count > 0 ? 4 : 0, pct)}%`,
                          backgroundColor: pos
                            ? 'var(--candle-bull)'
                            : 'var(--candle-bear)',
                        }}
                      />
                    </div>
                    <span className="whitespace-nowrap text-right" style={{ color: 'var(--text-primary)' }}>
                      {t('stats.rDist.tradeCount', { count: b.count })}
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
  return (
    <div
      className="rounded border p-3.5"
      style={{
        backgroundColor: 'var(--bg-surface)',
        borderColor: 'var(--border-hairline)',
      }}
    >
      <div className="text-[11px]" style={{ color: 'var(--text-secondary)' }}>
        {label}
      </div>
      <div
        className="mt-1 font-mono text-xl font-semibold tabular-nums"
        style={{ color: valueColor }}
      >
        {value}
      </div>
      <div className="mt-1 truncate font-mono text-[10px] tabular-nums" style={{ color: 'var(--text-muted)' }}>
        {sub}
      </div>
    </div>
  );
}
