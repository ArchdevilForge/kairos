import { t } from '../i18n';
import {
  EXIT_REASONS,
  GATE_CODES,
  REASON_CODES,
  SETUP_POINTS,
  type CycleAlignment,
  type CycleDirection,
  type ExitReason,
  type GateCode,
  type HumanDecision,
  type MarketState,
  type ReasonCode,
  type SeasonRegime,
  type SetupPoint,
  type TradeClass,
  type WavePhase,
} from '../types/kairos';

function defineDynamicGetters<K extends string, V>(
  keys: readonly K[],
  resolver: (key: K) => V,
): Record<K, V> {
  const target = {} as Record<K, V>;
  for (const key of keys) {
    Object.defineProperty(target, key, {
      enumerable: true,
      configurable: false,
      get() {
        return resolver(key);
      },
    });
  }
  return target;
}

function createDynamicPresetArray(getter: () => string[], length: number): string[] {
  const backing = new Array<string>(length).fill('');
  return new Proxy(backing, {
    get(_target, prop) {
      const current = getter();
      if (prop === 'length') return current.length;
      if (typeof prop === 'string') {
        const idx = Number(prop);
        if (Number.isInteger(idx) && idx >= 0 && idx < current.length && String(idx) === prop) {
          return current[idx];
        }
      }
      const value = Reflect.get(current, prop, current);
      return typeof value === 'function' ? value.bind(current) : value;
    },
    has(_target, prop) {
      return prop in getter();
    },
    ownKeys() {
      return Reflect.ownKeys(getter());
    },
    getOwnPropertyDescriptor(_target, prop) {
      return Reflect.getOwnPropertyDescriptor(getter(), prop);
    },
  });
}

export const REASON_CODE_LABELS: Record<ReasonCode, string> = defineDynamicGetters(
  REASON_CODES,
  (code) => t(`doctrine.reason.${code}`),
);

const SETUP_POINT_KEYS: Record<SetupPoint, string> = {
  POINT_1_BREAKOUT: '1',
  POINT_2_MINOR_DIV: '2',
  POINT_3_MID_TOP_SHORT: '3',
  POINT_4_SECOND_WAVE: '4',
  POINT_5_END_TOP_SHORT: '5',
  POINT_6_BOX_BOUNCE: '6',
};

export const SETUP_POINT_META: Record<
  SetupPoint,
  { key: string; short: string; label: string; hint: string }
> = defineDynamicGetters(SETUP_POINTS, (point) => ({
  key: SETUP_POINT_KEYS[point],
  short: t(`doctrine.setup.${point}.short`),
  label: t(`doctrine.setup.${point}.label`),
  hint: t(`doctrine.setup.${point}.hint`),
}));

const EXIT_REASON_KEYS: Record<ExitReason, string> = {
  PROTECTIVE_STOP: '1 / P',
  FAKEOUT_ABORT: '2 / F',
  STRUCTURAL_TRAIL: '3 / S',
  TARGET_PARTIAL: '4 / T',
  IMPULSE_EARLY: '5 / I',
};

export const EXIT_REASON_META: Record<
  ExitReason,
  { key: string; label: string; desc: string }
> = defineDynamicGetters(EXIT_REASONS, (reason) => ({
  key: EXIT_REASON_KEYS[reason],
  label: t(`doctrine.exit.${reason}.label`),
  desc: t(`doctrine.exit.${reason}.desc`),
}));

export const GATE_CODE_META: Record<
  string,
  { label: string; rule: string; nonOverridable?: boolean }
> = defineDynamicGetters<GateCode, { label: string; rule: string; nonOverridable?: boolean }>(
  GATE_CODES,
  (code) => ({
    label: t(`doctrine.gate.${code}.label`),
    rule: t(`doctrine.gate.${code}.rule`),
    ...(code === 'GATE_COOLDOWN_24H_LOCK' ? { nonOverridable: true } : {}),
  }),
);

const MARKET_STATE_TONES: Record<
  MarketState,
  'bull' | 'bear' | 'warn' | 'info' | 'muted'
> = {
  QUIET: 'muted',
  IMPULSE_UP: 'bull',
  IMPULSE_DOWN: 'bear',
  TRENDING_UP: 'bull',
  TRENDING_DOWN: 'bear',
  STRESS_UP: 'warn',
  STRESS_DOWN: 'warn',
  DECAY: 'info',
};

const MARKET_STATES: readonly MarketState[] = [
  'QUIET',
  'IMPULSE_UP',
  'IMPULSE_DOWN',
  'TRENDING_UP',
  'TRENDING_DOWN',
  'STRESS_UP',
  'STRESS_DOWN',
  'DECAY',
];

export const MARKET_STATE_META: Record<
  MarketState,
  { label: string; tone: 'bull' | 'bear' | 'warn' | 'info' | 'muted' }
> = defineDynamicGetters(MARKET_STATES, (state) => ({
  label: t(`doctrine.marketState.${state}`),
  tone: MARKET_STATE_TONES[state],
}));

const DIRECTIONS: readonly CycleDirection[] = ['up', 'down', 'neutral'];

export const DIRECTION_LABEL: Record<CycleDirection, string> = defineDynamicGetters(
  DIRECTIONS,
  (dir) => t(`doctrine.direction.${dir}`),
);

const WAVE_PHASES: readonly WavePhase[] = ['spring', 'summer', 'autumn', 'winter'];

export const PHASE_LABEL: Record<WavePhase, string> = defineDynamicGetters(
  WAVE_PHASES,
  (phase) => t(`doctrine.phase.${phase}`),
);

const SEASON_REGIMES: readonly SeasonRegime[] = [
  'SPRING',
  'SUMMER',
  'AUTUMN',
  'WINTER',
  '',
];

export const SEASON_LABEL: Record<SeasonRegime, string> = defineDynamicGetters(
  SEASON_REGIMES,
  (season) =>
    season === ''
      ? t('doctrine.season.unknown')
      : t(`doctrine.season.${season}`),
);

const CYCLE_ALIGNMENTS: readonly CycleAlignment[] = [
  'full_alignment',
  'counter_trend',
  'mixed',
  'no_trade',
];

export const ALIGNMENT_LABEL: Record<CycleAlignment, string> = defineDynamicGetters(
  CYCLE_ALIGNMENTS,
  (alignment) => t(`doctrine.alignment.${alignment}`),
);

const TRADE_CLASSES: readonly TradeClass[] = [
  'aligned_long',
  'aligned_short',
  'counter_trend_long',
  'counter_trend_short',
  'no_trade',
];

export const TRADE_CLASS_LABEL: Record<TradeClass, string> = defineDynamicGetters(
  TRADE_CLASSES,
  (tradeClass) => t(`doctrine.tradeClass.${tradeClass}`),
);

const HUMAN_DECISIONS: readonly HumanDecision[] = [
  'accepted',
  'waiting',
  'rejected',
  'missed',
];

export const DECISION_LABEL: Record<HumanDecision, string> = defineDynamicGetters(
  HUMAN_DECISIONS,
  (decision) => t(`doctrine.decision.${decision}`),
);

export function getReviewSetupTagPresets(): string[] {
  return [
    t('doctrine.preset.setup.0'),
    t('doctrine.preset.setup.1'),
    t('doctrine.preset.setup.2'),
    t('doctrine.preset.setup.3'),
    t('doctrine.preset.setup.4'),
    t('doctrine.preset.setup.5'),
  ];
}

export function getReviewErrorTagPresets(): string[] {
  return [
    t('doctrine.preset.error.0'),
    t('doctrine.preset.error.1'),
    t('doctrine.preset.error.2'),
    t('doctrine.preset.error.3'),
    t('doctrine.preset.error.4'),
    t('doctrine.preset.error.5'),
    t('doctrine.preset.error.6'),
  ];
}

export function getReviewEmotionPresets(): string[] {
  return [
    t('doctrine.preset.emotion.0'),
    t('doctrine.preset.emotion.1'),
    t('doctrine.preset.emotion.2'),
    t('doctrine.preset.emotion.3'),
    t('doctrine.preset.emotion.4'),
  ];
}

export const REVIEW_SETUP_TAG_PRESETS: string[] = createDynamicPresetArray(
  getReviewSetupTagPresets,
  6,
);

export const REVIEW_ERROR_TAG_PRESETS: string[] = createDynamicPresetArray(
  getReviewErrorTagPresets,
  7,
);

export const REVIEW_GRADE_PRESETS = ['A+', 'A', 'B', 'C', 'D'];

export const REVIEW_EMOTION_PRESETS: string[] = createDynamicPresetArray(
  getReviewEmotionPresets,
  5,
);

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
