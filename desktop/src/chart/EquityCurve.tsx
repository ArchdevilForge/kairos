import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
  AreaSeries,
  ColorType,
  LineSeries,
  createChart,
  type IChartApi,
  type ISeriesApi,
  type MouseEventParams,
  type TickMarkType,
  type Time,
} from 'lightweight-charts';
import { useI18n, type Locale, type TranslateFn } from '../i18n';
import type { EquityPoint } from '../types/kairos';
import { type AnthropicChartTheme, readChartTheme, withAlpha } from '../utils/chartTheme';
import {
  fmtChartCrosshairUtc8,
  fmtChartTickUtc8,
  fmtDateTimeSec,
  fmtPctPoints,
  fmtR,
  fmtUsd,
} from '../utils/format';

export interface EquityCurveProps {
  points: EquityPoint[];
  height?: number;
  emptyHint?: string;
  walletSummary?: string;
}

export default function EquityCurve({
  points,
  height = 240,
  emptyHint,
  walletSummary,
}: EquityCurveProps) {
  const { t, locale } = useI18n();
  const resolvedEmptyHint = emptyHint ?? t('equity.emptyHint');

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
  const themeRef = useRef<AnthropicChartTheme>(theme);
  themeRef.current = theme;
  const localeRef = useRef<Locale>(locale);
  localeRef.current = locale;
  const tRef = useRef<TranslateFn>(t);
  tRef.current = t;
  const hoveredTimeRef = useRef<number | null>(null);

  const sortedPoints = useMemo(() => {
    const byTime = new Map<number, EquityPoint>();
    for (const p of [...points].sort((a, b) => a.time - b.time)) {
      if (!Number.isFinite(p.time) || p.time <= 0) continue;
      byTime.set(p.time, p);
    }
    return [...byTime.values()];
  }, [points]);

  const hasEnoughPoints = sortedPoints.length >= 2;
  const pointsRef = useRef<EquityPoint[]>(sortedPoints);
  pointsRef.current = sortedPoints;

  const updateReadout = useCallback(
    (pt?: EquityPoint) => {
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
      bEl.textContent = t('equity.readoutBankroll', { value: fmtUsd(pt.bankroll_usd) });
      const hasWallet = pt.wallet_usd != null && Number.isFinite(pt.wallet_usd);
      wEl.textContent = t('equity.readoutWallet', {
        value: hasWallet ? fmtUsd(pt.wallet_usd) : '—',
      });
      wEl.style.color = hasWallet ? 'var(--accent-info)' : 'var(--text-muted)';
      pEl.textContent = t('equity.readoutTrade', { value: fmtUsd(pt.trade_pnl_usd, true) });
      pEl.style.color = pt.trade_pnl_usd >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)';
      rEl.textContent = t('equity.readoutCumR', { value: fmtR(pt.cumulative_r) });
      rEl.style.color = pt.cumulative_r >= 0 ? 'var(--pnl-pos)' : 'var(--pnl-neg)';
      dEl.textContent = t('equity.readoutDrawdown', {
        value: fmtPctPoints(-Math.abs(pt.drawdown_pct), 2, false),
      });
    },
    [t],
  );
  const updateReadoutRef = useRef(updateReadout);
  updateReadoutRef.current = updateReadout;

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
    if (!el || !hasEnoughPoints) return;

    const currentTheme = themeRef.current;
    const currentLocale = localeRef.current;
    const tr = tRef.current;

    const chart = createChart(el, {
      width: Math.max(1, el.clientWidth),
      height,
      layout: {
        background: { type: ColorType.Solid, color: currentTheme.bgSurface },
        textColor: currentTheme.textSecondary,
        fontFamily: currentTheme.fontMono,
        fontSize: 11,
      },
      grid: {
        vertLines: { color: currentTheme.gridLine },
        horzLines: { color: currentTheme.gridLine },
      },
      rightPriceScale: {
        borderColor: currentTheme.borderHairline,
        minimumWidth: 84,
      },
      timeScale: {
        borderColor: currentTheme.borderHairline,
        timeVisible: true,
        secondsVisible: false,
        tickMarkFormatter: (time: Time, tickMarkType: TickMarkType) =>
          fmtChartTickUtc8(time, tickMarkType),
      },
      localization: {
        locale: currentLocale === 'en' ? 'en-US' : 'zh-CN',
        timeFormatter: (time: Time) => fmtChartCrosshairUtc8(time),
      },
    });

    const bankrollSeries = chart.addSeries(
      AreaSeries,
      {
        lineColor: currentTheme.candleBull,
        topColor: withAlpha(currentTheme.candleBull, 0.28),
        bottomColor: withAlpha(currentTheme.candleBull, 0.02),
        lineWidth: 2,
        priceLineVisible: false,
        lastValueVisible: true,
        title: tr('equity.seriesBankroll'),
      },
      0,
    );

    const walletSeries = chart.addSeries(
      LineSeries,
      {
        color: currentTheme.accentInfo,
        lineWidth: 2,
        priceLineVisible: false,
        lastValueVisible: true,
        title: tr('equity.seriesWallet'),
      },
      0,
    );

    const ddSeries = chart.addSeries(
      AreaSeries,
      {
        lineColor: currentTheme.candleBear,
        topColor: withAlpha(currentTheme.candleBear, 0.04),
        bottomColor: withAlpha(currentTheme.candleBear, 0.28),
        lineWidth: 2,
        priceLineVisible: false,
        lastValueVisible: true,
        title: tr('equity.seriesDrawdown'),
      },
      0,
    );

    chartRef.current = chart;
    bankrollRef.current = bankrollSeries;
    walletRef.current = walletSeries;
    ddRef.current = ddSeries;

    const onCrosshair = (param: MouseEventParams<Time>) => {
      const list = pointsRef.current;
      if (param.time == null) {
        hoveredTimeRef.current = null;
        updateReadoutRef.current(list[list.length - 1]);
        return;
      }
      const ts = Number(param.time);
      hoveredTimeRef.current = ts;
      const match = list.find((p) => p.time === ts) ?? list[list.length - 1];
      updateReadoutRef.current(match);
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
  }, [height, hasEnoughPoints]);

  // Apply theme and locale updates live without recreating chart
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
      localization: {
        locale: locale === 'en' ? 'en-US' : 'zh-CN',
        timeFormatter: (time: Time) => fmtChartCrosshairUtc8(time),
      },
    });
    bankrollRef.current?.applyOptions({
      lineColor: theme.candleBull,
      topColor: withAlpha(theme.candleBull, 0.28),
      bottomColor: withAlpha(theme.candleBull, 0.02),
      title: t('equity.seriesBankroll'),
    });
    walletRef.current?.applyOptions({
      color: theme.accentInfo,
      title: t('equity.seriesWallet'),
    });
    ddRef.current?.applyOptions({
      lineColor: theme.candleBear,
      topColor: withAlpha(theme.candleBear, 0.04),
      bottomColor: withAlpha(theme.candleBear, 0.28),
      title: t('equity.seriesDrawdown'),
    });
  }, [theme, locale, t, hasEnoughPoints]);

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

    updateReadoutRef.current(sortedPoints[sortedPoints.length - 1]);
    chartRef.current?.timeScale().fitContent();
  }, [sortedPoints, mode]);

  // Refresh readout immediately when locale changes
  useEffect(() => {
    if (sortedPoints.length < 2) return;
    const hoverTs = hoveredTimeRef.current;
    const pt =
      hoverTs != null
        ? (sortedPoints.find((p) => p.time === hoverTs) ?? sortedPoints[sortedPoints.length - 1])
        : sortedPoints[sortedPoints.length - 1];
    updateReadout(pt);
  }, [sortedPoints, updateReadout]);

  if (!hasEnoughPoints) {
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
            {t('equity.title')}
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
          {resolvedEmptyHint}
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
            {t('equity.title')}
          </h2>
          <span className="flex shrink-0 items-center gap-1.5 text-[11px]" style={{ color: 'var(--candle-bull)' }}>
            <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: 'var(--candle-bull)' }} />
            {t('equity.legendBankroll')}
          </span>
          <span className="flex shrink-0 items-center gap-1.5 text-[11px]" style={{ color: 'var(--accent-info)' }}>
            <span className="inline-block h-2 w-2 rounded-full" style={{ backgroundColor: 'var(--accent-info)' }} />
            {t('equity.legendWallet')}
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
            {t('equity.modeEquity')}
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
            {t('equity.modeDrawdown')}
          </button>
        </div>
      </div>

      <div className="mb-2 flex h-[20px] shrink-0 items-center gap-3 overflow-hidden whitespace-nowrap font-mono text-[11px] tabular-nums">
        <span ref={timeSlotRef} className="w-[116px] shrink-0" style={{ color: 'var(--text-muted)' }} />
        <span ref={bankrollSlotRef} className="min-w-[126px] shrink-0" style={{ color: 'var(--text-primary)' }} />
        <span ref={walletSlotRef} className="min-w-[124px] shrink-0" style={{ color: 'var(--text-muted)' }} />
        <span ref={pnlSlotRef} className="min-w-[118px] shrink-0" style={{ color: 'var(--text-secondary)' }} />
        <span ref={cumRSlotRef} className="min-w-[104px] shrink-0" style={{ color: 'var(--text-secondary)' }} />
        <span ref={ddSlotRef} className="min-w-[104px] shrink-0" style={{ color: 'var(--pnl-neg)' }} />
      </div>

      <div ref={containerRef} style={{ height }} className="w-full" />
    </div>
  );
}
