import type {
  IPrimitivePaneRenderer,
  IPrimitivePaneView,
  ISeriesPrimitive,
  PrimitivePaneViewZOrder,
  SeriesAttachedParameter,
  Time,
} from 'lightweight-charts';
import type { CycleDirection } from '../types/kairos';
import { type AnthropicChartTheme, readChartTheme, withAlpha } from '../utils/chartTheme';
import { fmtPrice } from '../utils/format';
import type { SmcOverlayData } from '../utils/smc';

type DrawTarget = Parameters<IPrimitivePaneRenderer['draw']>[0];

export interface TicketOverlaySpec {
  smc?: SmcOverlayData | null;
  zoneLow?: number | null;
  zoneHigh?: number | null;
  signalAt?: number | null;
  entryPrice?: number | null;
  stopPrice?: number | null;
  targetPrice?: number | null;
  direction?: CycleDirection;
  lastBarTime?: number | null;
  firstBarTime?: number | null;
}

function drawSmcOverlay(
  ctx: CanvasRenderingContext2D,
  smc: SmcOverlayData,
  opts: {
    timeScale: ReturnType<SeriesAttachedParameter<Time>['chart']['timeScale']>;
    yForPrice: (p: number | null | undefined) => number | null;
    hRatio: number;
    vRatio: number;
    width: number;
    height: number;
    firstBarTime: number | null | undefined;
    lastBarTime: number | null | undefined;
    theme: AnthropicChartTheme;
  },
): void {
  const { timeScale, yForPrice, hRatio, vRatio, width, height, firstBarTime, lastBarTime, theme } =
    opts;

  // 1. Unmitigated Order Blocks (Swing & Internal, capped <= 5)
  const rawLast =
    lastBarTime != null && lastBarTime > 0 ? timeScale.timeToCoordinate(lastBarTime as Time) : null;
  const endX =
    rawLast != null && Number.isFinite(rawLast)
      ? Math.min(width, rawLast * hRatio + 18 * hRatio)
      : width;

  for (const ob of smc.orderBlocks) {
    const yTop = yForPrice(ob.top);
    const yBot = yForPrice(ob.bottom);
    if (yTop == null || yBot == null) continue;
    const topY = Math.min(yTop, yBot);
    const botY = Math.max(yTop, yBot);
    if (botY < 0 || topY > height) continue;

    const rawStart = timeScale.timeToCoordinate(ob.startTime as Time);
    let leftX: number;
    if (rawStart != null && Number.isFinite(rawStart)) {
      leftX = rawStart * hRatio;
    } else if (firstBarTime != null && ob.startTime <= firstBarTime) {
      leftX = 0;
    } else {
      continue;
    }
    if (leftX >= width) continue;

    const bx = Math.max(0, leftX);
    const rightX = Math.min(width, Math.max(bx + 8 * hRatio, endX));
    const bw = rightX - bx;
    if (bw <= 0) continue;

    const bh = Math.max(2 * vRatio, botY - topY);
    const baseColor = ob.direction === 'bull' ? theme.candleBull : theme.candleBear;
    const isSwing = ob.tier === 'swing';

    ctx.fillStyle = withAlpha(baseColor, isSwing ? 0.14 : 0.08);
    ctx.fillRect(bx, topY, bw, bh);

    ctx.strokeStyle = withAlpha(baseColor, isSwing ? 0.52 : 0.36);
    ctx.lineWidth = Math.max(1, vRatio);
    ctx.setLineDash(isSwing ? [] : [3 * hRatio, 3 * hRatio]);
    ctx.beginPath();
    ctx.moveTo(bx, topY);
    ctx.lineTo(rightX, topY);
    ctx.moveTo(bx, topY + bh);
    ctx.lineTo(rightX, topY + bh);
    ctx.stroke();

    if (bh >= 11 * vRatio && bw >= 36 * hRatio) {
      ctx.font = `${Math.round(9 * vRatio)}px ${theme.fontMono}`;
      ctx.fillStyle = withAlpha(baseColor, 0.85);
      ctx.textAlign = 'right';
      ctx.textBaseline = 'middle';
      ctx.fillText(isSwing ? 'OB' : 'iOB', rightX - 4 * hRatio, topY + bh / 2);
    }
  }

  // 2. BOS / CHoCH structure breaks
  for (const br of smc.breaks) {
    const c1 = timeScale.timeToCoordinate(br.startTime as Time);
    const c2 = timeScale.timeToCoordinate(br.endTime as Time);
    const y = yForPrice(br.level);
    if (c1 == null || c2 == null || y == null || !Number.isFinite(c1) || !Number.isFinite(c2)) {
      continue;
    }
    if (y < 0 || y > height) continue;
    const x1 = c1 * hRatio;
    const x2 = c2 * hRatio;
    if (x2 < 0 || x1 > width) continue;

    const cx1 = Math.max(0, x1);
    const cx2 = Math.min(width, x2);
    const color = br.direction === 'bull' ? theme.candleBull : theme.candleBear;
    const isSwing = br.tier === 'swing';

    ctx.strokeStyle = withAlpha(color, isSwing ? 0.85 : 0.68);
    ctx.lineWidth = Math.max(1, (isSwing ? 1.2 : 1) * vRatio);
    ctx.setLineDash(isSwing ? [] : [3 * hRatio, 3 * hRatio]);
    ctx.beginPath();
    ctx.moveTo(cx1, y);
    ctx.lineTo(cx2, y);
    ctx.stroke();

    if (cx2 - cx1 >= 26 * hRatio) {
      ctx.font = `${Math.round((isSwing ? 10 : 9) * vRatio)}px ${theme.fontMono}`;
      ctx.fillStyle = withAlpha(color, 0.9);
      ctx.textAlign = 'center';
      if (br.direction === 'bull') {
        ctx.textBaseline = 'bottom';
        ctx.fillText(br.kind, (cx1 + cx2) / 2, y - 3 * vRatio);
      } else {
        ctx.textBaseline = 'top';
        ctx.fillText(br.kind, (cx1 + cx2) / 2, y + 3 * vRatio);
      }
    }
  }

  // 3. EQH / EQL liquidity pools
  for (const eq of smc.equalLevels) {
    const c1 = timeScale.timeToCoordinate(eq.startTime as Time);
    const c2 = timeScale.timeToCoordinate(eq.endTime as Time);
    const y = yForPrice(eq.level);
    if (c1 == null || c2 == null || y == null || !Number.isFinite(c1) || !Number.isFinite(c2)) {
      continue;
    }
    if (y < 0 || y > height) continue;
    const x1 = c1 * hRatio;
    const x2 = c2 * hRatio;
    if (x2 < 0 || x1 > width) continue;

    const cx1 = Math.max(0, x1);
    const cx2 = Math.min(width, x2);
    ctx.strokeStyle = withAlpha(theme.stateWarn, 0.85);
    ctx.lineWidth = Math.max(1, vRatio);
    ctx.setLineDash([2 * hRatio, 3 * hRatio]);
    ctx.beginPath();
    ctx.moveTo(cx1, y);
    ctx.lineTo(cx2, y);
    ctx.stroke();

    if (cx2 - cx1 >= 22 * hRatio) {
      ctx.font = `${Math.round(9 * vRatio)}px ${theme.fontMono}`;
      ctx.fillStyle = withAlpha(theme.stateWarn, 0.92);
      ctx.textAlign = 'center';
      if (eq.kind === 'EQH') {
        ctx.textBaseline = 'bottom';
        ctx.fillText('EQH', (cx1 + cx2) / 2, y - 2 * vRatio);
      } else {
        ctx.textBaseline = 'top';
        ctx.fillText('EQL', (cx1 + cx2) / 2, y + 2 * vRatio);
      }
    }
  }

  ctx.setLineDash([]);
  ctx.textAlign = 'left';
  ctx.textBaseline = 'alphabetic';
}

class KairosOverlayRenderer implements IPrimitivePaneRenderer {
  private readonly spec: TicketOverlaySpec;
  private readonly param: SeriesAttachedParameter<Time> | null;
  private readonly theme: AnthropicChartTheme;

  constructor(
    spec: TicketOverlaySpec,
    param: SeriesAttachedParameter<Time> | null,
    theme: AnthropicChartTheme,
  ) {
    this.spec = spec;
    this.param = param;
    this.theme = theme;
  }

  draw(target: DrawTarget): void {
    const param = this.param;
    if (!param) return;
    const { chart, series } = param;
    const timeScale = chart.timeScale();

    target.useBitmapCoordinateSpace((scope) => {
      const ctx = scope.context;
      const hRatio = scope.horizontalPixelRatio;
      const vRatio = scope.verticalPixelRatio;
      const width = scope.bitmapSize.width;
      const height = scope.bitmapSize.height;

      const xForTime = (tsSec: number | null | undefined, fallbackX: number): number => {
        if (tsSec == null || !Number.isFinite(tsSec) || tsSec <= 0) return fallbackX;
        const coord = timeScale.timeToCoordinate(tsSec as Time);
        if (coord == null || !Number.isFinite(coord)) return fallbackX;
        return coord * hRatio;
      };

      const yForPrice = (p: number | null | undefined): number | null => {
        if (p == null || !Number.isFinite(p) || p <= 0) return null;
        const coord = series.priceToCoordinate(p);
        if (coord == null || !Number.isFinite(coord)) return null;
        return coord * vRatio;
      };

      ctx.save();

      if (this.spec.smc) {
        drawSmcOverlay(ctx, this.spec.smc, {
          timeScale,
          yForPrice,
          hRatio,
          vRatio,
          width,
          height,
          firstBarTime: this.spec.firstBarTime,
          lastBarTime: this.spec.lastBarTime,
          theme: this.theme,
        });
      }

      // Anchor X for EntryZone & 1R/2R/3R projections
      const anchorX = Math.max(
        0,
        Math.min(
          width,
          xForTime(this.spec.signalAt ?? this.spec.firstBarTime, 0),
        ),
      );
      const endX = width;

      // 2. EntryZone shading
      const zLow = this.spec.zoneLow;
      const zHigh = this.spec.zoneHigh;
      if (zLow != null && zHigh != null && zLow > 0 && zHigh > 0) {
        const y1 = yForPrice(Math.max(zLow, zHigh));
        const y2 = yForPrice(Math.min(zLow, zHigh));
        if (y1 != null && y2 != null) {
          const topY = Math.min(y1, y2);
          const bandH = Math.max(2 * vRatio, Math.abs(y2 - y1));
          ctx.fillStyle = withAlpha(this.theme.accentBrand, 0.11);
          ctx.fillRect(anchorX, topY, Math.max(1, endX - anchorX), bandH);

          ctx.strokeStyle = withAlpha(this.theme.accentBrand, 0.45);
          ctx.lineWidth = Math.max(1, vRatio);
          ctx.setLineDash([3 * hRatio, 3 * hRatio]);
          ctx.strokeRect(anchorX, topY, Math.max(1, endX - anchorX), bandH);
          ctx.setLineDash([]);
        }
      }

      // 3. Risk box (Entry <-> Stop) + 1R / 2R / 3R reward guide projections
      const entry = this.spec.entryPrice;
      const stop = this.spec.stopPrice;
      if (entry != null && stop != null && entry > 0 && stop > 0 && entry !== stop) {
        const yEntry = yForPrice(entry);
        const yStop = yForPrice(stop);
        const oneR = Math.abs(entry - stop);
        const isShort =
          this.spec.direction === 'down' ||
          (this.spec.direction !== 'up' && stop > entry);
        const dirSign = isShort ? -1 : 1;

        if (yEntry != null && yStop != null) {
          const rTop = Math.min(yEntry, yStop);
          const rH = Math.max(2 * vRatio, Math.abs(yStop - yEntry));
          ctx.fillStyle = withAlpha(this.theme.candleBear, 0.08);
          ctx.fillRect(anchorX, rTop, Math.max(1, endX - anchorX), rH);
        }

        // Shade reward zone between entry & 1R/target if targetPrice provided
        const targetP = this.spec.targetPrice;
        if (targetP != null && targetP > 0) {
          const yTarget = yForPrice(targetP);
          if (yEntry != null && yTarget != null) {
            const pTop = Math.min(yEntry, yTarget);
            const pH = Math.max(2 * vRatio, Math.abs(yTarget - yEntry));
            ctx.fillStyle = withAlpha(this.theme.candleBull, 0.07);
            ctx.fillRect(anchorX, pTop, Math.max(1, endX - anchorX), pH);
          }
        }

        ctx.font = `${Math.round(10 * vRatio)}px ${this.theme.fontMono}`;
        ctx.textBaseline = 'bottom';
        for (const mult of [1, 2, 3]) {
          const levelPrice = entry + dirSign * oneR * mult;
          if (levelPrice <= 0) continue;
          const yLv = yForPrice(levelPrice);
          if (yLv == null || yLv < 0 || yLv > height) continue;

          ctx.strokeStyle = withAlpha(this.theme.candleBull, mult === 2 ? 0.55 : 0.35);
          ctx.lineWidth = Math.max(1, vRatio);
          ctx.setLineDash([4 * hRatio, 4 * hRatio]);
          ctx.beginPath();
          ctx.moveTo(anchorX, yLv);
          ctx.lineTo(endX, yLv);
          ctx.stroke();
          ctx.setLineDash([]);

          ctx.fillStyle = withAlpha(this.theme.candleBull, 0.9);
          const lbl = `${mult}R ${fmtPrice(levelPrice)}`;
          const tw = ctx.measureText(lbl).width;
          ctx.fillText(lbl, Math.max(anchorX + 4 * hRatio, endX - tw - 10 * hRatio), yLv - 2 * vRatio);
        }
      }

      ctx.restore();
    });
  }
}

class KairosOverlayPaneView implements IPrimitivePaneView {
  private readonly getSpec: () => TicketOverlaySpec;
  private readonly getParam: () => SeriesAttachedParameter<Time> | null;
  private readonly getTheme: () => AnthropicChartTheme;

  constructor(
    getSpec: () => TicketOverlaySpec,
    getParam: () => SeriesAttachedParameter<Time> | null,
    getTheme: () => AnthropicChartTheme,
  ) {
    this.getSpec = getSpec;
    this.getParam = getParam;
    this.getTheme = getTheme;
  }

  zOrder(): PrimitivePaneViewZOrder {
    return 'bottom';
  }

  renderer(): IPrimitivePaneRenderer {
    return new KairosOverlayRenderer(this.getSpec(), this.getParam(), this.getTheme());
  }
}

/**
 * Single reusable ISeriesPrimitive for EntryZone and 1R/2R/3R projections.
 * Mutate via `setData()` which triggers `requestUpdate()` without detach/attach churn.
 */
export class KairosOverlayPrimitive implements ISeriesPrimitive<Time> {
  private _param: SeriesAttachedParameter<Time> | null = null;
  private _spec: TicketOverlaySpec = {};
  private _theme: AnthropicChartTheme = readChartTheme();
  private readonly _views: readonly IPrimitivePaneView[];

  constructor() {
    this._views = [
      new KairosOverlayPaneView(
        () => this._spec,
        () => this._param,
        () => this._theme,
      ),
    ];
  }

  attached(param: SeriesAttachedParameter<Time>): void {
    this._param = param;
  }

  detached(): void {
    this._param = null;
  }

  setData(spec: TicketOverlaySpec, theme?: AnthropicChartTheme): void {
    this._spec = spec;
    if (theme) this._theme = theme;
    this._param?.requestUpdate();
  }

  refreshTheme(theme: AnthropicChartTheme): void {
    this._theme = theme;
    this._param?.requestUpdate();
  }

  paneViews(): readonly IPrimitivePaneView[] {
    return this._views;
  }
}
