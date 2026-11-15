import { TickMarkType, type Time } from 'lightweight-charts';
import { t } from '../i18n';
import type { Timeframe } from '../types/kairos';

const UTC8_OFFSET_SEC = 8 * 3600;

export const TIMEFRAME_SECONDS: Record<Timeframe, number> = {
  '1m': 60,
  '5m': 300,
  '15m': 900,
  '1h': 3600,
  '4h': 14400,
  '1d': 86400,
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

/** Format Unix seconds with seconds precision (MM-DD HH:mm:ss, UTC+8). */
export function fmtDateTimeWithSec(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec) || sec <= 0) return '—';
  return dtfDateTimeFull.format(sec * 1000).replace(/\//g, '-');
}

/** Format ISO timestamp string (like Notice.ts) into UTC+8 YYYY-MM-DD HH:mm. */
export function fmtIsoUtc8(iso: string | null | undefined): string {
  if (!iso) return '—';
  const ms = Date.parse(iso);
  if (!Number.isFinite(ms)) return iso;
  return dtfDateTimeFull.format(ms).replace(/\//g, '-');
}

interface Utc8Parts {
  year: number;
  month: number;
  day: number;
  hour: number;
  minute: number;
  second: number;
}

function pad2(n: number): string {
  return n < 10 && n >= 0 ? `0${n}` : String(n);
}

/** Convert a Lightweight Charts Time value into Unix seconds. */
export function toUnixSecFromChartTime(time: Time): number | null {
  if (typeof time === 'number') {
    return Number.isFinite(time) ? time : null;
  }
  if (typeof time === 'string') {
    const ms = Date.parse(time);
    return Number.isFinite(ms) ? Math.floor(ms / 1000) : null;
  }
  if (time && typeof time === 'object' && 'year' in time && 'month' in time && 'day' in time) {
    const ms = Date.UTC(time.year, time.month - 1, time.day);
    return Number.isFinite(ms) ? Math.floor(ms / 1000) : null;
  }
  return null;
}

function toUtc8Parts(time: Time): Utc8Parts | null {
  const sec = toUnixSecFromChartTime(time);
  if (sec == null) return null;
  const shifted = new Date((sec + UTC8_OFFSET_SEC) * 1000);
  if (!Number.isFinite(shifted.getTime())) return null;
  return {
    year: shifted.getUTCFullYear(),
    month: shifted.getUTCMonth() + 1,
    day: shifted.getUTCDate(),
    hour: shifted.getUTCHours(),
    minute: shifted.getUTCMinutes(),
    second: shifted.getUTCSeconds(),
  };
}

/** Format chart X-axis tick mark strictly in Asia/Shanghai (UTC+8). */
export function fmtChartTickUtc8(time: Time, tickMarkType: TickMarkType): string {
  const p = toUtc8Parts(time);
  if (!p) return '';
  if (tickMarkType === TickMarkType.Year) {
    return String(p.year);
  }
  if (tickMarkType === TickMarkType.Month) {
    return `${p.year}-${pad2(p.month)}`;
  }
  if (p.hour === 0 && p.minute === 0 && p.second === 0) {
    return `${pad2(p.month)}-${pad2(p.day)}`;
  }
  if (tickMarkType === TickMarkType.DayOfMonth) {
    return `${pad2(p.month)}-${pad2(p.day)} ${pad2(p.hour)}:${pad2(p.minute)}`;
  }
  if (tickMarkType === TickMarkType.TimeWithSeconds) {
    return `${pad2(p.hour)}:${pad2(p.minute)}:${pad2(p.second)}`;
  }
  return `${pad2(p.hour)}:${pad2(p.minute)}`;
}

/** Format chart crosshair time label strictly in Asia/Shanghai (UTC+8). */
export function fmtChartCrosshairUtc8(time: Time): string {
  const p = toUtc8Parts(time);
  if (!p) return '';
  const base = `${p.year}-${pad2(p.month)}-${pad2(p.day)} ${pad2(p.hour)}:${pad2(p.minute)}`;
  return p.second !== 0 ? `${base}:${pad2(p.second)}` : base;
}

/** Format duration in seconds into concise localized text. */
export function fmtDurationSec(sec: number | null | undefined): string {
  if (sec == null || !Number.isFinite(sec) || sec < 0) return '—';
  if (sec < 60) return t('fmt.duration.sec', { s: Math.round(sec) });
  const totalMin = Math.round(sec / 60);
  if (totalMin < 60) return t('fmt.duration.min', { m: totalMin });
  const h = Math.floor(totalMin / 60);
  const m = totalMin % 60;
  if (h < 24) {
    return m > 0 ? t('fmt.duration.hourMin', { h, m }) : t('fmt.duration.hour', { h });
  }
  const d = Math.floor(h / 24);
  const rh = h % 24;
  return rh > 0 ? t('fmt.duration.dayHour', { d, h: rh }) : t('fmt.duration.day', { d });
}


