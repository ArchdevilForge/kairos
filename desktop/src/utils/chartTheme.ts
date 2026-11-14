export interface AnthropicChartTheme {
  mode: 'dark' | 'light';
  bgCanvas: string;
  bgSurface: string;
  bgElevated: string;
  textPrimary: string;
  textSecondary: string;
  textMuted: string;
  borderHairline: string;
  gridLine: string;
  accentBrand: string;
  candleBull: string;
  candleBullWick: string;
  candleBear: string;
  candleBearWick: string;
  accentInfo: string;
  stateWarn: string;
  fontMono: string;
  fontSans: string;
  rulerFill: string;
  rulerStroke: string;
  rulerLabelBg: string;
  rulerLabelText: string;
}

function readVar(styles: CSSStyleDeclaration, name: string, fallback: string): string {
  const val = styles.getPropertyValue(name).trim();
  return val || fallback;
}

/** Apply an alpha multiplier to #rrggbb or rgb/rgba color strings cleanly. */
export function withAlpha(color: string, alpha: number): string {
  const a = Math.max(0, Math.min(1, alpha));
  const c = color.trim();
  if (c.startsWith('#')) {
    const hex = c.slice(1);
    if (hex.length === 3) {
      const r = parseInt(hex[0] + hex[0], 16);
      const g = parseInt(hex[1] + hex[1], 16);
      const b = parseInt(hex[2] + hex[2], 16);
      return `rgba(${r}, ${g}, ${b}, ${a})`;
    }
    if (hex.length >= 6) {
      const r = parseInt(hex.slice(0, 2), 16);
      const g = parseInt(hex.slice(2, 4), 16);
      const b = parseInt(hex.slice(4, 6), 16);
      return `rgba(${r}, ${g}, ${b}, ${a})`;
    }
  }
  const m = c.match(/^rgba?\(\s*([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)/i);
  if (m) {
    return `rgba(${m[1]}, ${m[2]}, ${m[3]}, ${a})`;
  }
  return c;
}

/** Read Anthropic CSS variables from :root for lightweight-charts and Canvas2D layers. */
export function readChartTheme(): AnthropicChartTheme {
  const isLight =
    typeof document !== 'undefined' && document.documentElement.dataset.theme === 'light';
  const mode: 'dark' | 'light' = isLight ? 'light' : 'dark';

  const defaults = isLight
    ? {
        bgCanvas: '#FAF9F5',
        bgSurface: '#F5F4ED',
        bgElevated: '#E8E6DC',
        textPrimary: '#141413',
        textSecondary: '#5E5D59',
        textMuted: '#87867F',
        borderHairline: 'rgba(20, 20, 19, 0.10)',
        gridLine: 'rgba(20, 20, 19, 0.05)',
        accentBrand: '#D97757',
        candleBull: '#788C5D',
        candleBullWick: '#5B6E44',
        candleBear: '#B85450',
        candleBearWick: '#A84440',
        accentInfo: '#4E7DAA',
        stateWarn: '#A6792E',
      }
    : {
        bgCanvas: '#141413',
        bgSurface: '#1C1B19',
        bgElevated: '#262522',
        textPrimary: '#FAF9F5',
        textSecondary: '#B0AEA5',
        textMuted: '#87867F',
        borderHairline: 'rgba(250, 249, 245, 0.09)',
        gridLine: 'rgba(250, 249, 245, 0.045)',
        accentBrand: '#D97757',
        candleBull: '#788C5D',
        candleBullWick: '#8FA670',
        candleBear: '#B85450',
        candleBearWick: '#D16D68',
        accentInfo: '#6A9BCC',
        stateWarn: '#C99A4E',
      };

  if (typeof window === 'undefined' || typeof document === 'undefined') {
    return {
      mode,
      ...defaults,
      fontMono: '"JetBrains Mono", ui-monospace, monospace',
      fontSans: '"Inter", system-ui, sans-serif',
      rulerFill: withAlpha(defaults.accentBrand, 0.12),
      rulerStroke: defaults.accentBrand,
      rulerLabelBg: defaults.bgElevated,
      rulerLabelText: defaults.textPrimary,
    };
  }

  const styles = getComputedStyle(document.documentElement);
  const bgCanvas = readVar(styles, '--bg-canvas', defaults.bgCanvas);
  const bgSurface = readVar(styles, '--bg-surface', defaults.bgSurface);
  const bgElevated = readVar(styles, '--bg-elevated', defaults.bgElevated);
  const textPrimary = readVar(styles, '--text-primary', defaults.textPrimary);
  const textSecondary = readVar(styles, '--text-secondary', defaults.textSecondary);
  const textMuted = readVar(styles, '--text-muted', defaults.textMuted);
  const borderHairline = readVar(styles, '--border-hairline', defaults.borderHairline);
  const gridLine = readVar(styles, '--chart-grid', defaults.gridLine);
  const accentBrand = readVar(styles, '--accent-brand', defaults.accentBrand);
  const candleBull = readVar(styles, '--candle-bull', defaults.candleBull);
  const candleBear = readVar(styles, '--candle-bear', defaults.candleBear);
  const pnlPos = readVar(styles, '--pnl-pos', defaults.candleBullWick);
  const pnlNeg = readVar(styles, '--pnl-neg', defaults.candleBearWick);
  const accentInfo = readVar(styles, '--accent-info', defaults.accentInfo);
  const stateWarn = readVar(styles, '--state-warn', defaults.stateWarn);
  const fontMono = readVar(styles, '--font-mono', '"JetBrains Mono", ui-monospace, monospace');
  const fontSans = readVar(styles, '--font-sans', '"Inter", system-ui, sans-serif');

  return {
    mode,
    bgCanvas,
    bgSurface,
    bgElevated,
    textPrimary,
    textSecondary,
    textMuted,
    borderHairline,
    gridLine,
    accentBrand,
    candleBull,
    candleBullWick: pnlPos,
    candleBear,
    candleBearWick: pnlNeg,
    accentInfo,
    stateWarn,
    fontMono,
    fontSans,
    rulerFill: withAlpha(accentBrand, 0.12),
    rulerStroke: accentBrand,
    rulerLabelBg: bgElevated,
    rulerLabelText: textPrimary,
  };
}
