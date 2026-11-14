import type { Time } from 'lightweight-charts';
import type { ChartDrawingItem, ChartDrawingPoint } from '../types/kairos';
import type { AnthropicChartTheme } from './chartTheme';
import { withAlpha } from './chartTheme';
import { fmtDateTimeWithSec, fmtDurationSec, fmtPrice } from './format';

export type RulerCorner = {
  time: Time;
  price: number;
  x: number;
  y: number;
  logical?: number;
};

export type RulerMeasure = {
  priceDelta: number;
  pricePct: number;
  bars: number;
  durationSec: number;
};

export type Point2D = { x: number; y: number };

export type DrawingVisualState = 'normal' | 'hover' | 'selected' | 'preview';
export type DrawingHitPart = 'p0' | 'p1' | 'p01' | 'p10' | 'body';

export interface DrawingHitResult {
  drawing: ChartDrawingItem;
  part: DrawingHitPart;
}

const HANDLE_HIT_RADIUS_PX = 10;

export function timeToUnixSec(t: Time): number | null {
  if (typeof t === 'number' && Number.isFinite(t)) return t;
  if (typeof t === 'object' && t && 'year' in t) {
    const { year, month, day } = t;
    return Math.floor(Date.UTC(year, month - 1, day) / 1000);
  }
  return null;
}

export function measureRuler(
  a: RulerCorner,
  b: RulerCorner,
  stepSec: number,
): RulerMeasure {
  const pa = a.price;
  const pb = b.price;
  const priceDelta = pb - pa;
  const pricePct = pa !== 0 ? (priceDelta / pa) * 100 : 0;
  const ta = timeToUnixSec(a.time);
  const tb = timeToUnixSec(b.time);
  let durationSec = 0;
  if (ta != null && tb != null) {
    durationSec = Math.abs(tb - ta);
  }
  let bars = 0;
  if (
    a.logical != null &&
    b.logical != null &&
    Number.isFinite(a.logical) &&
    Number.isFinite(b.logical)
  ) {
    bars = Math.round(Math.abs(b.logical - a.logical));
  } else if (stepSec > 0) {
    bars = Math.round(durationSec / stepSec);
  }
  return { priceDelta, pricePct, bars, durationSec };
}

export function syncOverlayCanvasSize(
  canvas: HTMLCanvasElement,
  container: HTMLElement,
): { width: number; height: number } {
  const dpr = typeof window !== 'undefined' ? window.devicePixelRatio || 1 : 1;
  const width = Math.max(1, container.clientWidth);
  const height = Math.max(1, container.clientHeight);
  const targetW = Math.floor(width * dpr);
  const targetH = Math.floor(height * dpr);
  if (canvas.width !== targetW || canvas.height !== targetH) {
    canvas.style.width = `${width}px`;
    canvas.style.height = `${height}px`;
    canvas.width = targetW;
    canvas.height = targetH;
  }
  const ctx = canvas.getContext('2d');
  if (ctx) {
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  }
  return { width, height };
}

export function clearRulerCanvas(canvas: HTMLCanvasElement | null): void {
  if (!canvas) return;
  const ctx = canvas.getContext('2d');
  if (!ctx) return;
  ctx.save();
  ctx.setTransform(1, 0, 0, 1, 0, 0);
  ctx.clearRect(0, 0, canvas.width, canvas.height);
  ctx.restore();
}

function drawArrowHead(
  ctx: CanvasRenderingContext2D,
  tipX: number,
  tipY: number,
  ux: number,
  uy: number,
  size = 5,
): void {
  const px = -uy;
  const py = ux;
  ctx.beginPath();
  ctx.moveTo(tipX, tipY);
  ctx.lineTo(tipX - ux * size + px * (size * 0.55), tipY - uy * size + py * (size * 0.55));
  ctx.moveTo(tipX, tipY);
  ctx.lineTo(tipX - ux * size - px * (size * 0.55), tipY - uy * size - py * (size * 0.55));
  ctx.stroke();
}

export function drawRulerOnCanvas(
  ctx: CanvasRenderingContext2D,
  width: number,
  height: number,
  a: RulerCorner,
  b: RulerCorner,
  measure: RulerMeasure,
  theme: AnthropicChartTheme,
): void {
  const left = Math.min(a.x, b.x);
  const right = Math.max(a.x, b.x);
  const top = Math.min(a.y, b.y);
  const bottom = Math.max(a.y, b.y);

  const isUp = measure.priceDelta >= 0;
  const tone = isUp ? theme.candleBull : theme.candleBear;

  ctx.save();
  ctx.fillStyle = withAlpha(tone, 0.14);
  ctx.strokeStyle = withAlpha(tone, 0.72);
  ctx.lineWidth = 1;
  ctx.fillRect(left, top, right - left, bottom - top);
  ctx.strokeRect(left, top, right - left, bottom - top);

  const midX = (a.x + b.x) / 2;
  const midY = (a.y + b.y) / 2;
  const dx = b.x - a.x;
  const dy = b.y - a.y;

  // TradingView-style horizontal & vertical measurement cross-axes with arrowheads
  ctx.strokeStyle = tone;
  ctx.setLineDash([4, 3]);
  if (Math.abs(dx) >= 4) {
    ctx.beginPath();
    ctx.moveTo(a.x, midY);
    ctx.lineTo(b.x, midY);
    ctx.stroke();
    if (Math.abs(dx) >= 14) {
      ctx.setLineDash([]);
      drawArrowHead(ctx, b.x, midY, Math.sign(dx), 0);
      ctx.setLineDash([4, 3]);
    }
  }
  if (Math.abs(dy) >= 4) {
    ctx.beginPath();
    ctx.moveTo(midX, a.y);
    ctx.lineTo(midX, b.y);
    ctx.stroke();
    if (Math.abs(dy) >= 14) {
      ctx.setLineDash([]);
      drawArrowHead(ctx, midX, b.y, 0, Math.sign(dy));
    }
  }
  ctx.setLineDash([]);

  for (const pt of [a, b]) {
    ctx.fillStyle = theme.bgCanvas;
    ctx.strokeStyle = tone;
    ctx.lineWidth = 1.5;
    ctx.beginPath();
    ctx.arc(pt.x, pt.y, 3, 0, Math.PI * 2);
    ctx.fill();
    ctx.stroke();
  }

  const sign = isUp ? '+' : '-';
  const deltaText = `${sign}${fmtPrice(Math.abs(measure.priceDelta))}`;
  const pctText = `${isUp ? '+' : ''}${measure.pricePct.toFixed(2)}%`;
  const label = `${deltaText} (${pctText}) · ${measure.bars} 根 · ${fmtDurationSec(measure.durationSec)}`;

  ctx.font = `11px ${theme.fontMono}`;
  const padX = 8;
  const tw = ctx.measureText(label).width;
  const boxW = tw + padX * 2;
  const boxH = 20;
  const lx = Math.min(Math.max(4, left + 4), Math.max(4, width - boxW - 4));
  const ly = top > boxH + 6 ? top - boxH - 4 : Math.min(height - boxH - 4, bottom + 4);

  ctx.fillStyle = theme.bgElevated;
  ctx.strokeStyle = withAlpha(tone, 0.55);
  ctx.lineWidth = 1;
  ctx.fillRect(lx, ly, boxW, boxH);
  ctx.strokeRect(lx, ly, boxW, boxH);

  ctx.fillStyle = theme.textPrimary;
  ctx.textBaseline = 'middle';
  ctx.fillText(label, lx + padX, ly + boxH / 2);
  ctx.restore();
}

function drawHandle(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  strokeColor: string,
  theme: AnthropicChartTheme,
): void {
  ctx.fillStyle = theme.bgCanvas;
  ctx.strokeStyle = strokeColor;
  ctx.lineWidth = 1.75;
  ctx.setLineDash([]);
  ctx.beginPath();
  ctx.arc(x, y, 4.2, 0, Math.PI * 2);
  ctx.fill();
  ctx.stroke();
}

export function drawPersistedDrawing(
  ctx: CanvasRenderingContext2D,
  width: number,
  height: number,
  drawing: Pick<ChartDrawingItem, 'kind' | 'points' | 'label'>,
  mapPoint: (p: ChartDrawingPoint) => Point2D | null,
  priceToY: (price: number) => number | null,
  theme: AnthropicChartTheme,
  state: DrawingVisualState = 'normal',
  stepSec = 300,
): void {
  const isPreview = state === 'preview';
  const showHandles = state === 'hover' || state === 'selected' || isPreview;
  const strokeColor =
    state === 'selected' || isPreview
      ? theme.accentBrand
      : state === 'hover'
        ? theme.textPrimary
        : theme.accentInfo;

  ctx.save();

  if (drawing.kind === 'hline' && drawing.points.length >= 1) {
    const y = priceToY(drawing.points[0].price);
    if (y == null || !Number.isFinite(y)) {
      ctx.restore();
      return;
    }
    ctx.strokeStyle = strokeColor;
    ctx.lineWidth = state === 'selected' ? 2 : 1.35;
    ctx.setLineDash(isPreview ? [5, 4] : []);
    ctx.beginPath();
    ctx.moveTo(0, y);
    ctx.lineTo(width, y);
    ctx.stroke();

    if (showHandles) {
      drawHandle(ctx, width * 0.5, y, strokeColor, theme);
    }

    const text = drawing.label
      ? `${drawing.label} ${fmtPrice(drawing.points[0].price)}`
      : fmtPrice(drawing.points[0].price);
    ctx.font = `10px ${theme.fontMono}`;
    const tw = ctx.measureText(text).width;
    const py = Math.max(2, y - 16);
    ctx.fillStyle = theme.bgElevated;
    ctx.fillRect(8, py, tw + 10, 14);
    ctx.fillStyle = strokeColor;
    ctx.textBaseline = 'middle';
    ctx.fillText(text, 13, py + 7);
    ctx.restore();
    return;
  }

  const pts = drawing.points.map(mapPoint);
  if (pts.length < 2 || !pts[0] || !pts[1]) {
    ctx.restore();
    return;
  }
  const [a, b] = pts;

  if (drawing.kind === 'ruler') {
    const aCorner: RulerCorner = {
      time: drawing.points[0].time as Time,
      price: drawing.points[0].price,
      x: a.x,
      y: a.y,
    };
    const bCorner: RulerCorner = {
      time: drawing.points[1].time as Time,
      price: drawing.points[1].price,
      x: b.x,
      y: b.y,
    };
    drawRulerOnCanvas(
      ctx,
      width,
      height,
      aCorner,
      bCorner,
      measureRuler(aCorner, bCorner, stepSec),
      theme,
    );
    if (showHandles) {
      drawHandle(ctx, a.x, a.y, theme.accentBrand, theme);
      drawHandle(ctx, b.x, b.y, theme.accentBrand, theme);
    }
    ctx.restore();
    return;
  }

  ctx.restore();
}

function hitTestSingleDrawing(
  d: ChartDrawingItem,
  x: number,
  y: number,
  isSelected: boolean,
  mapPoint: (p: ChartDrawingPoint) => Point2D | null,
  priceToY: (price: number) => number | null,
  tolerancePx: number,
): DrawingHitPart | null {
  if (d.kind === 'hline' && d.points.length >= 1) {
    const ly = priceToY(d.points[0].price);
    return ly != null && Math.abs(y - ly) <= tolerancePx ? 'body' : null;
  }
  if (d.points.length < 2) return null;
  const a = mapPoint(d.points[0]);
  const b = mapPoint(d.points[1]);
  if (!a || !b) return null;

  if (Math.hypot(x - a.x, y - a.y) <= HANDLE_HIT_RADIUS_PX) return 'p0';
  if (Math.hypot(x - b.x, y - b.y) <= HANDLE_HIT_RADIUS_PX) return 'p1';

  if (d.kind === 'ruler') {
    if (Math.hypot(x - a.x, y - b.y) <= HANDLE_HIT_RADIUS_PX) return 'p01';
    if (Math.hypot(x - b.x, y - a.y) <= HANDLE_HIT_RADIUS_PX) return 'p10';
    const left = Math.min(a.x, b.x);
    const right = Math.max(a.x, b.x);
    const top = Math.min(a.y, b.y);
    const bottom = Math.max(a.y, b.y);
    if (
      x < left - tolerancePx ||
      x > right + tolerancePx ||
      y < top - tolerancePx ||
      y > bottom + tolerancePx
    ) {
      return null;
    }
    if (isSelected) return 'body';
    const nearHoriz = Math.abs(y - top) <= tolerancePx || Math.abs(y - bottom) <= tolerancePx;
    const nearVert = Math.abs(x - left) <= tolerancePx || Math.abs(x - right) <= tolerancePx;
    return nearHoriz || nearVert ? 'body' : null;
  }

  return null;
}

/** Hit-test persisted drawings and identify whether a corner/endpoint handle or body was hit. */
export function hitTestDrawingDetailed(
  x: number,
  y: number,
  _width: number,
  drawings: readonly ChartDrawingItem[],
  mapPoint: (p: ChartDrawingPoint) => Point2D | null,
  priceToY: (price: number) => number | null,
  selectedId: number | null = null,
  tolerancePx = 8,
): DrawingHitResult | null {
  if (selectedId != null) {
    const sel = drawings.find((d) => d.id === selectedId);
    if (sel) {
      const part = hitTestSingleDrawing(sel, x, y, true, mapPoint, priceToY, tolerancePx);
      if (part) return { drawing: sel, part };
    }
  }
  for (let i = drawings.length - 1; i >= 0; i -= 1) {
    const d = drawings[i];
    if (d.id === selectedId) continue;
    const part = hitTestSingleDrawing(d, x, y, false, mapPoint, priceToY, tolerancePx);
    if (part) return { drawing: d, part };
  }
  return null;
}

/** Hit-test persisted drawings at cursor `(x, y)` for individual hover / single-click delete. */
export function hitTestDrawing(
  x: number,
  y: number,
  width: number,
  drawings: readonly ChartDrawingItem[],
  mapPoint: (p: ChartDrawingPoint) => Point2D | null,
  priceToY: (price: number) => number | null,
  tolerancePx = 8,
): ChartDrawingItem | null {
  return hitTestDrawingDetailed(x, y, width, drawings, mapPoint, priceToY, null, tolerancePx)?.drawing ?? null;
}

export function drawReplayCursor(
  ctx: CanvasRenderingContext2D,
  width: number,
  height: number,
  x: number,
  cursorTs: number,
  theme: AnthropicChartTheme,
): void {
  if (!Number.isFinite(x)) return;
  ctx.save();
  ctx.strokeStyle = theme.accentBrand;
  ctx.lineWidth = 1.25;
  ctx.setLineDash([3, 3]);
  ctx.beginPath();
  ctx.moveTo(x + 0.5, 0);
  ctx.lineTo(x + 0.5, height);
  ctx.stroke();
  ctx.setLineDash([]);

  const label = fmtDateTimeWithSec(cursorTs);
  ctx.font = `11px ${theme.fontMono}`;
  const tw = ctx.measureText(label).width;
  const pillW = tw + 14;
  const pillH = 18;
  const lx = Math.min(Math.max(4, x - pillW / 2), Math.max(4, width - pillW - 4));
  const ly = Math.max(4, height - pillH - 4);

  ctx.fillStyle = theme.accentBrand;
  ctx.fillRect(lx, ly, pillW, pillH);
  ctx.fillStyle = theme.bgCanvas;
  ctx.textBaseline = 'middle';
  ctx.fillText(label, lx + 7, ly + pillH / 2);
  ctx.restore();
}

export function drawPriceDragPreview(
  ctx: CanvasRenderingContext2D,
  width: number,
  y: number,
  title: string,
  price: number,
  color: string,
  theme: AnthropicChartTheme,
): void {
  if (!Number.isFinite(y)) return;
  ctx.save();
  ctx.strokeStyle = color;
  ctx.lineWidth = 1.5;
  ctx.setLineDash([6, 4]);
  ctx.beginPath();
  ctx.moveTo(0, y + 0.5);
  ctx.lineTo(width, y + 0.5);
  ctx.stroke();
  ctx.setLineDash([]);

  const label = `${title} ${fmtPrice(price)}`;
  ctx.font = `11px ${theme.fontMono}`;
  const tw = ctx.measureText(label).width;
  const boxW = tw + 14;
  const boxH = 18;
  const ly = Math.max(4, y - boxH / 2);

  ctx.fillStyle = theme.bgElevated;
  ctx.strokeStyle = color;
  ctx.fillRect(8, ly, boxW, boxH);
  ctx.strokeRect(8, ly, boxW, boxH);
  ctx.fillStyle = theme.textPrimary;
  ctx.textBaseline = 'middle';
  ctx.fillText(label, 15, ly + boxH / 2);
  ctx.restore();
}
