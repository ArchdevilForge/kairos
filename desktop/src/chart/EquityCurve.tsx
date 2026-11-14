import { useEffect, useMemo, useRef, useState } from 'react';
import {
  AreaSeries,
  ColorType,
  LineSeries,
  createChart,
  type IChartApi,
  type ISeriesApi,
  type MouseEventParams,
  type Time,
} from 'lightweight-charts';
import type { EquityPoint } from '../types/kairos';
import { type AnthropicChartTheme, readChartTheme, withAlpha } from '../utils/chartTheme';
import { fmtDateTimeSec, fmtPctPoints, fmtR, fmtUsd } from '../utils/format';

export interface EquityCurveProps {
  points: EquityPoint[];
  height?: number;
  emptyHint?: string;
  walletSummary?: string;
}

export default function EquityCurve({
  points,
  height = 240,
  emptyHint = '暂无足够已结算样本以绘制权益与钱包对账曲线。',
  walletSummary,
}: EquityCurveProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const chartRef = useRef<IChartApi | null>(null);
  const bankrollRef = useRef<ISeriesApi<'Area'> | null>(null);
  const walletRef = useRef<ISeriesApi<'Line'> | null>(null);
  const ddRef = useRef<ISeriesApi<'Area'> | null>(null);
  const timeSlotRef = useRef<HTMLSpanElement>(null);
  const bankrollSlotRef = useRef<HTMLSpanElement>(null);
  const walletSlotRef = useRef<HTMLSpanElement>(null);
  const pnlSlotRef = useRef<HTMLSpanElement>(null);
  const cumRSlotRef = useRef<HTMLSpanElement>(null);
  const ddSlotRef = useRef<HTMLSpanElement>(null);

  const [mode, setMode] = useState<'equity' | 'drawdown'>('equity');
  const [theme, setTheme] = useState<AnthropicChartTheme>(() => readChartTheme());

  const sortedPoints = useMemo(() => {
    const byTime = new Map<number, EquityPoint>();
    for (const p of [...points].sort((a, b) => a.time - b.time)) {
      if (!Number.isFinite(p.time) || p.time <= 0) continue;
      byTime.set(p.time, p);
    }
    return [...byTime.values()];
  }, [points]);

  const pointsRef = useRef<EquityPoint[]>(sortedPoints);
  pointsRef.current = sortedPoints;

  useEffect(() => {
    const observer = new MutationObserver(() => setTheme(readChartTheme()));
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['data-theme'],
    });
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const el = containerRef.current;
    if (!el || sortedPoints.length < 2) return;

    const chart = createChart(el, {
      width: Math.max(1, el.clientWidth),
      height,
      layout: {
        background: { type: ColorType.Solid, color: theme.bgSurface },
        textColor: theme.textSecondary,
        fontFamily: theme.fontMono,
        fontSize: 11,
      },
      grid: {
        vertLines: { color: theme.gridLine },
        horzLines: { color: theme.gridLine },
      },
      rightPriceScale: {
        borderColor: theme.borderHairline,
        minimumWidth: 84,
      },
      timeScale: {
        borderColor: theme.borderHairline,
        timeVisible: true,
        secondsVisible: false,
      },
    });

    const bankrollSeries = chart.addSeries(
      AreaSeries,
      {
        lineColor: theme.candleBull,
        topColor: withAlpha(theme.candleBull, 0.28),
        bottomColor: withAlpha(theme.candleBull, 0.02),
        lineWidth: 2,
        priceLineVisible: false,
        lastValueVisible: true,
        title: 'Bankroll',
      },
      0,
    );

    const walletSeries = chart.addSeries(
      LineSeries,
      {
        color: theme.accentInfo,
        lineWidth: 2,
        priceLineVisible: false,
        lastValueVisible: true,
        title: 'Wallet',
      },
      0,
    );

    const ddSeries = chart.addSeries(
      AreaSeries,
      {
        lineColor: theme.candleBear,
        topColor: withAlpha(theme.candleBear, 0.04),
        bottomColor: withAlpha(theme.candleBear, 0.28),
        lineWidth: 2,
        priceLineVisible: false,
        lastValueVisible: true,
        title: 'DD%',
      },
      0,
    );

    chartRef.current = chart;
    bankrollRef.current = bankrollSeries;
    walletRef.current = walletSeries;
    ddRef.current = ddSeries;

    const updateReadout = (pt?: EquityPoint) => {
      const tEl = timeSlotRef.current;
      const bEl = bankrollSlotRef.current;
      const wEl = walletSlotRef.current;
      const pEl = pnlSlotRef.current;
      const rEl = cumRSlotRef.current;
      const dEl = ddSlotRef.current;
      if (!tEl || !bEl || !wEl || !pEl || !rEl || !dEl) return;
      if (!pt) {
        tEl.textContent = '';
        bEl.textContent = '';
        wEl.textContent = '';
        pEl.textContent = '';
        rEl.textContent = '';
        dEl.textContent = '';
        return;
      }
      tEl.textContent = fmtDateTimeSec(pt.time);
      bEl.textContent = `账面 ${fmtUsd(pt.bankroll_usd)}`;
      const hasWallet = pt.wallet_usd != null && Number.isFinite(pt.wallet_usd);
      wEl.textContent = hasWallet ? `钱包 ${fmtUsd(pt.wallet_usd)}` : '钱包 —';
      wEl.style.color = hasWallet ? 'var(--accent-info)' : 'var(--text-muted)';
      pEl.textContent = `单笔 ${fmtUsd(pt.trade_pnl_usd, true)}`;
      pEl.style.color = pt.trade_pnl_usd >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)';
      rEl.textContent = `累计 ${fmtR(pt.cumulative_r)}`;
      rEl.style.color = pt.cumulative_r >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)';
      dEl.textContent = `回撤 ${fmtPctPoints(-Math.abs(pt.drawdown_pct), 2, false)}`;
    };

    const onCrosshair = (param: MouseEventParams<Time>) => {
      const list = pointsRef.current;
      if (param.time == null) {
        updateReadout(list[list.length - 1]);
        return;
      }
      const t = Number(param.time);
      const match = list.find((p) => p.time === t) ?? list[list.length - 1];
      updateReadout(match);
    };

    chart.subscribeCrosshairMove(onCrosshair);

    const ro = new ResizeObserver(() => {
      if (!containerRef.current || containerRef.current.clientWidth === 0) return;
      chart.applyOptions({
        width: containerRef.current.clientWidth,
        height,
      });
    });
    ro.observe(el);

    return () => {
      ro.disconnect();
      chart.unsubscribeCrosshairMove(onCrosshair);
      chart.remove();
      chartRef.current = null;
      bankrollRef.current = null;
      walletRef.current = null;
      ddRef.current = null;
    };
  }, [height, sortedPoints.length >= 2]);

  // Apply theme
  useEffect(() => {
    const chart = chartRef.current;
    if (!chart) return;
    chart.applyOptions({
      layout: {
        background: { type: ColorType.Solid, color: theme.bgSurface },
        textColor: theme.textSecondary,
        fontFamily: theme.fontMono,
      },
      grid: {
        vertLines: { color: theme.gridLine },
        horzLines: { color: theme.gridLine },
      },
      rightPriceScale: { borderColor: theme.borderHairline },
      timeScale: { borderColor: theme.borderHairline },
    });
    bankrollRef.current?.applyOptions({
      lineColor: theme.candleBull,
      topColor: withAlpha(theme.candleBull, 0.28),
      bottomColor: withAlpha(theme.candleBull, 0.02),
    });
    walletRef.current?.applyOptions({ color: theme.accentInfo });
    ddRef.current?.applyOptions({
      lineColor: theme.candleBear,
      topColor: withAlpha(theme.candleBear, 0.04),
      bottomColor: withAlpha(theme.candleBear, 0.28),
    });
  }, [theme]);

  // Populate series data
  useEffect(() => {
    if (!bankrollRef.current || !walletRef.current || !ddRef.current) return;
    if (sortedPoints.length < 2) return;

    if (mode === 'equity') {
      bankrollRef.current.setData(
        sortedPoints.map((p) => ({ time: p.time as Time, value: p.bankroll_usd })),
      );
      walletRef.current.setData(
        sortedPoints
          .filter((p) => p.wallet_usd != null && Number.isFinite(p.wallet_usd))
          .map((p) => ({ time: p.time as Time, value: p.wallet_usd! })),
      );
      ddRef.current.setData([]);
    } else {
      bankrollRef.current.setData([]);
      walletRef.current.setData([]);
      ddRef.current.setData(
        sortedPoints.map((p) => ({
          time: p.time as Time,
          value: -Math.abs(p.drawdown_pct),
        })),
      );
    }

    const last = sortedPoints[sortedPoints.length - 1];
    if (last && timeSlotRef.current && bankrollSlotRef.current && walletSlotRef.current && pnlSlotRef.current && cumRSlotRef.current && ddSlotRef.current) {
      timeSlotRef.current.textContent = fmtDateTimeSec(last.time);
      bankrollSlotRef.current.textContent = `账面 ${fmtUsd(last.bankroll_usd)}`;
      const hasWallet = last.wallet_usd != null && Number.isFinite(last.wallet_usd);
      walletSlotRef.current.textContent = hasWallet ? `钱包 ${fmtUsd(last.wallet_usd)}` : '钱包 —';
      walletSlotRef.current.style.color = hasWallet ? 'var(--accent-info)' : 'var(--text-muted)';
      pnlSlotRef.current.textContent = `单笔 ${fmtUsd(last.trade_pnl_usd, true)}`;
      pnlSlotRef.current.style.color = last.trade_pnl_usd >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)';
      cumRSlotRef.current.textContent = `累计 ${fmtR(last.cumulative_r)}`;
      cumRSlotRef.current.style.color = last.cumulative_r >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)';
      ddSlotRef.current.textContent = `回撤 ${fmtPctPoints(-Math.abs(last.drawdown_pct), 2, false)}`;
    }
    chartRef.current?.timeScale().fitContent();
  }, [sortedPoints, mode]);

  if (sortedPoints.length < 2) {
    return (
      <div
        className="rounded border p-4"
        style={{
          backgroundColor: 'var(--bg-surface)',
          borderColor: 'var(--border-hairline)',
        }}
      >
        <div className="mb-2 flex h-[28px] shrink-0 items-center justify-between gap-3 overflow-hidden whitespace-nowrap">
          <h2 className="shrink-0 font-editorial text-base font-medium" style={{ color: 'var(--text-primary)' }}>
            资金净值曲线
          </h2>
          {walletSummary ? (
            <span className="truncate font-mono text-[11px] tabular-nums" style={{ color: 'var(--text-secondary)' }}>
              {walletSummary}
            </span>
          ) : null}
        </div>
        <div
          className="flex items-center justify-center rounded border px-4 py-10 text-xs"
          style={{
            height,
            backgroundColor: 'var(--bg-canvas)',
            borderColor: 'var(--border-hairline)',
            color: 'var(--text-muted)',
          }}
        >
          {emptyHint}
        </div>
      </div>
    );
  }

  return (
    <div
      className="flex flex-col rounded border p-4"
      style={{
        backgroundColor: 'var(--bg-surface)',
        borderColor: 'var(--border-hairline)',
      }}
    >
      <div className="mb-1.5 flex h-[28px] shrink-0 items-center justify-between gap-3 overflow-hidden whitespace-nowrap">
        <div className="flex min-w-0 items-center gap-3">
          <h2 className="shrink-0 font-editorial text-base font-medium" style={{ color: 'var(--text-primary)' }}>
            资金净值曲线
          </h2>
          <span className="flex shrink-0 items-center gap-1.5 text-[11px]" style={{ color: 'var(--candle-bull)' }}>
            <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: 'var(--candle-bull)' }} />
            Trading Bankroll
          </span>
          <span className="flex shrink-0 items-center gap-1.5 text-[11px]" style={{ color: 'var(--accent-info)' }}>
            <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: 'var(--accent-info)' }} />
            Read-Only Wallet
          </span>
          {walletSummary ? (
            <span className="truncate font-mono text-[11px] tabular-nums" style={{ color: 'var(--text-secondary)' }}>
              {walletSummary}
            </span>
          ) : null}
        </div>

        <div
          className="flex shrink-0 rounded border p-0.5 text-[11px]"
          style={{
            backgroundColor: 'var(--bg-canvas)',
            borderColor: 'var(--border-hairline)',
          }}
        >
          <button
            type="button"
            onClick={() => setMode('equity')}
            className="rounded px-2 py-0.5 transition-colors"
            style={{
              backgroundColor: mode === 'equity' ? 'var(--bg-elevated)' : 'transparent',
              color: mode === 'equity' ? 'var(--accent-brand)' : 'var(--text-secondary)',
            }}
          >
            资金曲线
          </button>
          <button
            type="button"
            onClick={() => setMode('drawdown')}
            className="rounded px-2 py-0.5 transition-colors"
            style={{
              backgroundColor: mode === 'drawdown' ? 'var(--bg-elevated)' : 'transparent',
              color: mode === 'drawdown' ? 'var(--candle-bear)' : 'var(--text-secondary)',
            }}
          >
            回撤 %
          </button>
        </div>
      </div>

      <div className="mb-2 flex h-[20px] shrink-0 items-center gap-3 overflow-hidden whitespace-nowrap font-mono text-[11px] tabular-nums">
        <span ref={timeSlotRef} className="w-[116px] shrink-0" style={{ color: 'var(--text-muted)' }} />
        <span ref={bankrollSlotRef} className="w-[112px] shrink-0" style={{ color: 'var(--text-primary)' }} />
        <span ref={walletSlotRef} className="w-[118px] shrink-0" style={{ color: 'var(--text-muted)' }} />
        <span ref={pnlSlotRef} className="w-[116px] shrink-0" style={{ color: 'var(--text-secondary)' }} />
        <span ref={cumRSlotRef} className="w-[100px] shrink-0" style={{ color: 'var(--text-secondary)' }} />
        <span ref={ddSlotRef} className="w-[104px] shrink-0" style={{ color: 'var(--pnl-neg)' }} />
      </div>

      <div ref={containerRef} style={{ height }} className="w-full" />
    </div>
  );
}
