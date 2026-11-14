import type { Timeframe } from '../types/kairos';

export const TIMEFRAME_SECONDS: Record<Timeframe, number> = {
  '1m': 60,
  '5m': 300,
  '15m': 900,
  '1h': 3600,
  '4h': 14400,
  '1d': 86400,
};

export const TIMEFRAME_MS: Record<Timeframe, number> = {
  '1m': 60_000,
  '5m': 300_000,
  '15m': 900_000,
  '1h': 3_600_000,
  '4h': 14_400_000,
  '1d': 86_400_000,
};

const dtfDateTimeSec = new Intl.DateTimeFormat('zh-CN', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  hourCycle: 'h23',
});

const dtfDateTimeFull = new Intl.DateTimeFormat('zh-CN', {
  timeZone: 'Asia/Shanghai',
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
});

const dtfTimeShort = new Intl.DateTimeFormat('zh-CN', {
  timeZone: 'Asia/Shanghai',
  hour: '2-digit',
  minute: '2-digit',
  second: '2-digit',
  hourCycle: 'h23',
});

const dtfDateKey = new Intl.DateTimeFormat('zh-CN', {
  timeZone: 'Asia/Shanghai',
  year: 'numeric',
  month: '2-digit',
  day: '2-digit',
});

/** Adaptive price precision so sub-cent coins stay readable without scientific notation. */
export function fmtPrice(price: number | null | undefined): string {
  if (price == null || !Number.isFinite(price)) return '—';
  const abs = Math.abs(price);
  if (abs === 0) return '0.00';
  if (abs >= 1000) return price.toFixed(2);
  if (abs >= 1) return price.toFixed(4);
  if (abs >= 1e-7) {
    const decimals = Math.min(8, Math.max(4, 3 - Math.floor(Math.log10(abs))));
    return price.toFixed(decimals);
  }
  return price.toFixed(8);
}

/** Format USD amount with 2 decimals. */
export function fmtUsd(v: number | null | undefined, signed = false): string {
  if (v == null || !Number.isFinite(v)) return '—';
  const absStr = Math.abs(v).toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
  if (v < 0) return `-$${absStr}`;
  if (signed && v > 0) return `+$${absStr}`;
  return `$${absStr}`;
}

/** Format compact USD volume / OI (e.g. $12.4M, $840K). */
export function fmtCompactUsd(v: number | null | undefined): string {
  if (v == null || !Number.isFinite(v)) return '—';
  const abs = Math.abs(v);
  const sign = v < 0 ? '-' : '';
  if (abs >= 1e9) return `${sign}$${(abs / 1e9).toFixed(2)}B`;
  if (abs >= 1e6) return `${sign}$${(abs / 1e6).toFixed(2)}M`;
  if (abs >= 1e3) return `${sign}$${(abs / 1e3).toFixed(1)}K`;
  return `${sign}$${abs.toFixed(0)}`;
}

/** Format R-multiple (e.g. +1.85R, -1.00R). */
export function fmtR(v: number | null | undefined, signed = true, decimals = 2): string {
  if (v == null || !Number.isFinite(v)) return '—';
  const sign = signed && v > 0 ? '+' : '';
  return `${sign}${v.toFixed(decimals)}R`;
}

/** Format a value already expressed in percentage points (e.g. 0.75 -> 0.75%). */
export function fmtPctPoints(
  v: number | null | undefined,
  decimals = 2,
  signed = true,
): string {
  if (v == null || !Number.isFinite(v)) return '—';
  const sign = signed && v > 0 ? '+' : '';
  return `${sign}${v.toFixed(decimals)}%`;
}

/** Format a 0..1 ratio as percentage (e.g. 0.429 -> 42.9%). */
export function fmtRatioPct(
  v: number | null | undefined,
  decimals = 1,
  signed = false,
): string {
  if (v == null || !Number.isFinite(v)) return '—';
  const pct = v * 100;
  const sign = signed && pct > 0 ? '+' : '';
  return `${sign}${pct.toFixed(decimals)}%`;
}

/** Format perp funding rate (fraction like 0.0001 -> +0.0100%). */
export function fmtFundingRate(v: number | null | undefined): string {
  if (v == null || !Number.isFinite(v)) return '—';
  const pct = v * 100;
  const sign = pct > 0 ? '+' : '';
  return `${sign}${pct.toFixed(4)}%`;
}

/** Format contract quantity with adaptive precision. */
export function fmtQty(v: number | null | undefined): string {
  if (v == null || !Number.isFinite(v)) return '—';
  const abs = Math.abs(v);
  if (abs >= 1000) return v.toFixed(1);
  if (abs >= 1) return v.toFixed(3);
  return v.toFixed(5);
}

/** Format Unix timestamp (seconds) in strict Asia/Shanghai (UTC+8). */
export function fmtDateTimeSec(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec) || sec <= 0) return '—';
  return dtfDateTimeSec.format(sec * 1000).replace(/\//g, '-');
}

/** Format Unix timestamp (milliseconds) in strict Asia/Shanghai (UTC+8). */
export function fmtDateTimeMs(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms <= 0) return '—';
  return dtfDateTimeSec.format(ms).replace(/\//g, '-');
}

/** Format Unix seconds with seconds precision (MM-DD HH:mm:ss, UTC+8). */
export function fmtDateTimeWithSec(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec) || sec <= 0) return '—';
  return dtfDateTimeFull.format(sec * 1000).replace(/\//g, '-');
}

/** Format HH:mm:ss in UTC+8 from Unix seconds. */
export function fmtTimeShortSec(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec) || sec <= 0) return '—';
  return dtfTimeShort.format(sec * 1000);
}

/** Format ISO timestamp string (like Notice.ts) into UTC+8 YYYY-MM-DD HH:mm. */
export function fmtIsoUtc8(iso: string | null | undefined): string {
  if (!iso) return '—';
  const ms = Date.parse(iso);
  if (!Number.isFinite(ms)) return iso;
  return dtfDateTimeFull.format(ms).replace(/\//g, '-');
}

/** Format YYYY-MM-DD in UTC+8 from Unix seconds. */
export function fmtDateKeyUtc8(sec: number): string {
  return dtfDateKey.format(sec * 1000).replace(/\//g, '-');
}

/** Return UTC+8 hour (0..23) for a Unix second timestamp. */
export function hourUtc8FromSec(sec: number): number {
  const shifted = new Date((sec + 8 * 3600) * 1000);
  return shifted.getUTCHours();
}

/** Format duration in seconds into concise Chinese text. */
export function fmtDurationSec(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec) || sec < 0) return '—';
  if (sec < 60) return `${Math.round(sec)} 秒`;
  const totalMin = Math.round(sec / 60);
  if (totalMin < 60) return `${totalMin} 分`;
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  if (h < 24) return m > 0 ? `${h} 时 ${m} 分` : `${h} 时`;
  const d = Math.floor(h / 24);
  const rh = h % 24;
  return rh > 0 ? `${d} 天 ${rh} 时` : `${d} 天`;
}

/** Format duration in milliseconds. */
export function fmtDurationMs(ms: number | null | undefined): string {
  if (ms == null || !Number.isFinite(ms) || ms < 0) return '—';
  return fmtDurationSec(ms / 1000);
}

/** Shorten perpetual symbol label e.g. BTC/USDT:USDT -> BTC/USDT. */
export function fmtSymbolShort(sym: string): string {
  if (!sym) return '—';
  return sym.replace(/:USDT$/i, '');
}
