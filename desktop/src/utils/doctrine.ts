import { t, type Locale } from '../i18n';
import type { DeskStatus, WavePhase } from '../types/kairos';

export type AccountExchange = 'bybit' | 'binance';

/** Resolve active read-only account exchange, defaulting to Bybit. */
export function resolveAccountExchange(
  status?: Pick<DeskStatus, 'exchange' | 'account'> | null,
): AccountExchange {
  const raw = (status?.account?.exchange || status?.exchange || 'bybit')
    .trim()
    .toLowerCase();
  return raw === 'binance' ? 'binance' : 'bybit';
}

/** Return localized display name for an exchange. */
export function formatExchangeLabel(
  exchange: AccountExchange,
  locale: Locale,
): string {
  if (exchange === 'binance') {
    return locale === 'zh' ? '币安' : 'Binance';
  }
  return 'Bybit';
}

const WAVE_PHASES: readonly WavePhase[] = ['spring', 'summer', 'autumn', 'winter'];

export const PHASE_LABEL: Record<WavePhase, string> = (() => {
  const target = {} as Record<WavePhase, string>;
  for (const phase of WAVE_PHASES) {
    Object.defineProperty(target, phase, {
      enumerable: true,
      configurable: false,
      get() {
        return t(`doctrine.phase.${phase}`);
      },
    });
  }
  return target;
})();

/** Normalize symbol strings across BTC/USDT:USDT and BTCUSDT for resilient comparisons. */
export function normalizeSymbolKey(sym: string): string {
  return sym
    .toUpperCase()
    .replace(':USDT', '')
    .replace(/[^A-Z0-9]/g, '');
}

export function sameSymbol(a: string | undefined, b: string | undefined): boolean {
  if (!a || !b) return false;
  return normalizeSymbolKey(a) === normalizeSymbolKey(b);
}
