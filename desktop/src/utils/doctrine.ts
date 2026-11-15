import { t } from '../i18n';
import type { WavePhase } from '../types/kairos';

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
