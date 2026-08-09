# Maker Fill Modeling for Prediction-Market / CLOB Backtests

## Query

Research practical maker fill modeling for prediction-market or CLOB backtests, especially thin-tail binary markets like Polymarket. Include common conventions, simple implementable models, pitfalls, and how this maps to pm-bot's existing `FillModel`, backtest engine, and paper daemon.

## Files Found

| File Path | Description |
|-----------|-------------|
| `pm_bot/backtest/costs.py` | Contains the current `FillModel` dataclass and `CostModel` integration. |
| `pm_bot/backtest/engine.py` | Applies `FillModel` in live-mode maker backtests using Bernoulli sampling and records unfilled simulated trades. |
| `pm_bot/core/paper_trade.py` | Paper-trade persistence currently records dry-run signals as open paper positions. |
| `pm_bot/cli/daemon.py` | Dry-run daemon execution path records paper trades immediately; live path posts GTC limit orders through `ClobTrader`. |
| `pm_bot/core/clob.py` | CLOB wrapper supports GTC limit buy/sell, FOK market buy, cancel, open-order lookup, order-status lookup, and trade lookup. |
| `.trellis/tasks/05-06-live-backtest-fidelity/prd.md` | Current task requirements: align paper/live daemon behavior with live-mode backtest assumptions, including maker fills and stop-loss. |
| `tests/test_backtest_costs.py` | Unit coverage for current `FillModel` probability behavior and `CostModel` defaults. |
| `tests/test_backtest_engine.py` | Unit coverage for stop-loss fields and live-mode fill-model integration. |
| `tests/test_cli_daemon.py` | Existing daemon tests around trade execution paths. |

## Current pm-bot Fill / Execution Behavior

### `FillModel` in `pm_bot/backtest/costs.py`

Lines 8-30 define a simple price-band probability model:

```python
@dataclass
class FillModel:
    fill_prob_at_best: float = 0.50
    fill_prob_inside: float = 0.25
    fill_prob_tail: float = 0.10
    tail_low: float = 0.01
    tail_high: float = 0.15
    tail_high2: float = 0.85
    tail_very_high: float = 0.99

    def fill_probability(self, price: float) -> float:
        if price <= self.tail_low or price >= self.tail_very_high:
            return self.fill_prob_tail
        if price <= self.tail_high or price >= self.tail_high2:
            return self.fill_prob_tail
        return self.fill_prob_at_best
```

Observed behavior:

- Prices in `<=0.15` or `>=0.85` are classified as tail and receive `fill_prob_tail`.
- Prices outside `[0.01, 0.99]` also receive `fill_prob_tail`.
- All non-tail prices return `fill_prob_at_best`.
- `fill_prob_inside` exists but is not currently used by `fill_probability` because the method does not receive quote-relative context such as best bid/ask, spread, or order offset.

`CostModel.__init__` creates `self.fill_model = FillModel()` at lines 53-54.

### Backtest application in `pm_bot/backtest/engine.py`

The backtest engine applies the model only when both conditions hold:

- `self.live_mode` is true.
- `side == "maker"`, where `side = self.costs.live_side if self.live_mode else "taker"`.

In per-strategy mode, lines 338-363 do:

```python
fill_prob = self.costs.fill_model.fill_probability(effective_price)
filled = self._rng.random() < fill_prob
if not filled:
    skip_count += 1
    trade = SimulatedTrade(..., cost=0.0, pnl=0.0, filled=False)
    trades.append(trade)
    continue
fill_count += 1
```

Portfolio mode has the same pattern at lines 579-603.

Observed behavior:

- Unfilled maker orders are represented as `SimulatedTrade(... filled=False)` with zero cost and zero P&L.
- Filled trades continue into normal cost and P&L calculation.
- Live-mode fill/skip totals are logged with `log.info("fill_model_stats", filled=fill_count, skipped=skip_count)` at line 435.
- Sampling is deterministic when the engine is constructed with a seed; CLI exposes `--seed` for deterministic FillModel sampling in `pm_bot/cli/app.py` and `pm_bot/cli/backtest_cmd.py`.

### Paper daemon behavior in `pm_bot/cli/daemon.py` and `pm_bot/core/paper_trade.py`

Dry-run `_execute_trade` currently records every eligible signal as a paper trade immediately:

```python
if self.dry_run and self.paper is not None:
    shares = size_usd / price if price > 0 else 1
    self.paper.record_trade(...)
    self.trades_this_cycle += 1
    log.info("dry_run_trade", ...)
    return
```

`PaperTradeDB.record_trade` inserts rows into `paper_trades` with default `status = 'open'` and increments `daily_spent` by `size_usd`.

Observed paper state model:

- Schema columns include `order_id`, `market_id`, `strategy`, `side`, `price`, `size_usd`, `shares`, `kelly_fraction`, `edge`, `city`, `temp_label`, `reasoning`, `status`, `settled_pnl`, `created_at`, `settled_at`.
- There is no separate order lifecycle state for `pending`, `unfilled`, `partially_filled`, `cancelled`, or `expired` in the current schema.
- `check_duplicate_order` only blocks duplicates where `status = 'open'`.
- `get_city_spent` and `get_total_exposure` sum rows with `status IN ('open', 'filled')`, although inserted dry-run rows default to `open`.
- Paper settlement reads `status = 'open'` rows and then marks them `settled`.

### Live CLOB behavior in `pm_bot/core/clob.py`

Relevant methods:

- `place_limit_buy` posts a `BUY` order as `OrderType.GTC`.
- `place_limit_sell` posts a `SELL` order as `OrderType.GTC`.
- `place_market_buy` posts a market buy as `OrderType.FOK`.
- `cancel_order`, `cancel_all_orders`, `get_open_orders`, `get_order_status`, and `get_trades` are available wrappers.

The live daemon path records an order after successful limit-order submission via `TradeDB.record_trade`, then later `_poll_fills` consults `get_order_status` for open trades.

## External Findings: Common Conventions

### Prediction-market execution stack

Polymarket-focused backtesting material commonly separates three layers:

1. Signal layer: historical prices and derived features.
2. Tradeability layer: spread, depth, volume, market activity, and whether the market is liquid at signal time.
3. Execution layer: fill price, fill ratio, slippage, partial fills, and rejected/unfilled orders.

This convention appears in PolymarketData's 1-minute backtesting guide, which explicitly maps `/prices` to signal data, `/metrics` to tradeability, and `/books` to execution.

### Taker / aggressive fills

The common simple model for taker or marketable orders is L2 depth walking:

```python
def weighted_fill(levels, target_size):
    remaining = float(target_size)
    filled = notional = 0.0
    for price, size in levels:
        take = min(remaining, float(size))
        notional += take * float(price)
        filled += take
        remaining -= take
        if remaining <= 0:
            break
    if filled == 0:
        return None, 0.0, float(target_size)
    avg_fill = notional / filled
    unfilled = max(0.0, float(target_size) - filled)
    return avg_fill, filled, unfilled
```

Conventions around this model:

- Walk asks for buys; walk bids for sells.
- Return `avg_fill`, `filled_quantity`, and `unfilled_quantity`.
- Track `fill_ratio = filled / requested`.
- Compute slippage from a fixed reference price, usually midpoint or best quote.
- Keep the reference definition stable across runs.

PolyTest gives the same convention in TypeScript: walk orderbook levels, consume visible liquidity, compute average fill price, and report slippage.

### Maker / passive limit fills

For maker orders, full L2 depth alone is not enough unless historical trades and queue state are available. Common conventions include:

1. **Observed-trade / queue model**  
   Place the simulated order in the queue, estimate queue ahead at the order price, and fill only as same-side marketable trade flow consumes quantity ahead. hftbacktest describes probability queue-position models and notes that different queue models should be calibrated by comparing backtest and live results.

2. **Simple queue-position approximation**  
   Without exact own queue position, assign a starting queue fraction such as front / middle / back, then decrement queue-ahead using subsequent trade volume at or through the order price. Partial fills occur when trade volume exceeds queue ahead but not full order size.

3. **Hazard / Bernoulli model**  
   When only top-of-book or sparse snapshots are available, model fill as a probability over an order's resting window. Probability is conditioned on order price, tail region, spread, book depth, time-to-resolution, and whether the order is at best bid/ask or inside the spread.

4. **Time-in-force window model**  
   Treat a maker order as resting for `ttl` seconds or one daemon cycle. If not filled by then, mark unfilled/expired/cancelled. Polymarket docs describe GTC/GTD as passive resting orders and FOK/FAK as immediate-or-cancel styles for aggressive rebalancing.

### Polymarket order-lifecycle conventions

Polymarket market-maker docs state:

- GTC rests on the book until filled or cancelled.
- GTD auto-expires at a specified time and is useful before known events.
- FOK must fill entirely immediately or cancel.
- FAK fills immediately available quantity and cancels the rest.
- Market makers should cancel stale quotes, monitor open orders, and subscribe to user-channel fill notifications.
- Makers are not charged fees; most taker-enabled markets charge taker fees.
- Prices must conform to tick size.

These conventions map directly to pm-bot's live CLOB wrapper, which currently uses GTC for limit buy/sell and FOK for market buy.

## Simple Implementable Models

### Model A: Existing static Bernoulli price-band model

Inputs:

- `price`
- `tail_low`, `tail_high`, `tail_high2`, `tail_very_high`
- `fill_prob_at_best`, `fill_prob_tail`

Behavior:

- Tail binary prices receive a low fill probability.
- Middle prices receive a higher fill probability.
- One random draw determines filled vs unfilled.

This is exactly the current backtest model in `pm_bot/backtest/costs.py` and `pm_bot/backtest/engine.py`.

Implementation properties:

- Does not require orderbook data.
- Deterministic with seeded RNG.
- Produces all-or-nothing fills.
- Does not represent partial fills.
- Does not distinguish at-best vs inside-spread despite having `fill_prob_inside` as a field.

### Model B: Quote-context Bernoulli model

Inputs:

- order price
- side/direction
- best bid and best ask at signal time
- spread
- order offset from best quote
- tail-price band
- optional time-to-resolution / market age

Typical probability rules:

- At current best bid/ask: base maker probability.
- One tick inside spread: lower probability if price improvement creates queue priority but also less adverse crossing flow in thin books, or higher probability if inside quote becomes marketable soon; convention must be calibrated to actual data.
- Far from best quote: lower probability.
- Tail prices near 0/1: lower probability because thin-tail binary markets can show stale quotes and sparse contra flow.
- Wide spread or old book snapshot: lower probability or block trade.

Mapping to pm-bot:

- `FillModel.fill_probability(price)` would need quote context to use `fill_prob_inside` meaningfully.
- Existing daemon recommendations contain `rec.price`, `rec.direction`, and bucket prices, but dry-run execution currently does not pass best bid/ask or spread into a fill model.

### Model C: Cycle-window Bernoulli + order lifecycle

Inputs:

- static or quote-context probability
- daemon cycle id / timestamp
- order TTL in cycles or seconds
- seeded RNG

Behavior:

- On signal, insert a simulated order as pending/open-order rather than immediately filled.
- Sample fill over the order's allowed rest window.
- If the draw fails or TTL expires, mark unfilled/expired and do not count exposure/P&L.
- If the draw succeeds, mark filled/open-position and count exposure/P&L.

Mapping to pm-bot:

- This model is the closest paper-mode analog to the existing backtest Bernoulli model.
- It can reuse the current probability values while aligning dry-run behavior with the backtest's filled/unfilled distinction.
- Current `paper_trades` schema would need a way to distinguish orders from positions if implemented in storage; currently all dry-run inserts are `open` positions.

### Model D: L2 depth model for taker / marketable execution

Inputs:

- historical or current orderbook side levels
- target size in shares/contracts
- side: buy or sell
- reference price

Behavior:

- Walk asks for buy, bids for sell.
- Fill as much visible depth as available.
- Return average fill, filled size, unfilled size, fill ratio, and slippage.

Mapping to pm-bot:

- This is more applicable to taker backtests or stop-loss exits than passive maker entries.
- `CostModel.calculate_cost(side="taker", ...)` currently uses synthetic spread/slippage/fee assumptions rather than visible L2 depth.
- `ClobTrader.place_market_buy` uses FOK, whose all-or-none semantics differ from FAK partial-fill behavior.

### Model E: Queue-depletion maker model

Inputs:

- order price and size
- order placement timestamp
- estimated queue ahead at the same price
- subsequent trade prints at or through the order price
- cancellation/expiration time

Behavior:

- Initial queue-ahead is visible size at the chosen price multiplied by a queue-position fraction.
- Subsequent contra-side marketable volume first consumes queue ahead.
- Remaining volume fills the simulated order partially or fully.
- Unfilled remainder persists until cancelled/expired.

Mapping to pm-bot:

- This requires trade replay or live fill events, not just current `rec.price`.
- It matches hftbacktest-style queue modeling and marketlens-style `queue_position` execution realism, but is outside the current minimal `FillModel` shape.

## Thin-Tail Binary Market Pitfalls

### Tail prices near 0/1

Thin-tail binary markets often show prices near 0.01-0.15 or 0.85-0.99 where displayed quotes may be stale, contra flow is sparse, and apparent edge can be dominated by one-cent tick effects. pm-bot's current `FillModel` explicitly encodes this by lowering fill probability in these bands.

### Midpoint / best-quote overstatement

Polymarket-specific guides repeatedly warn that midpoint fills overstate performance. Even best-quote assumptions can be too optimistic when target size exceeds top-level depth. For marketable execution, L2 depth walking is the common transparent baseline.

### Partial fills

A requested order size can exceed available or realized contra flow. Common reporting uses:

- requested size
- filled size
- unfilled size
- fill ratio
- average fill price

Current pm-bot backtest Bernoulli fills are all-or-nothing and current paper dry-run rows are all filled/open positions.

### Snapshot lag

When matching signal time to book time, PolymarketData highlights tracking `t_signal - t_book` and treating large lags as unknown. This matters in fast event windows where books change quickly.

### Event-window spread shocks

Prediction-market spreads can widen materially near catalysts or resolution windows. A model using average spread or static slippage can miss these event-window regimes.

### Side asymmetry

Buy and sell slippage/fill behavior can differ on the same market due to sentiment, inventory, and event proximity. Pooling YES and NO or buy and sell fills can hide this asymmetry.

### Look-ahead bias

Backtests must process point-in-time data chronologically. Do not use future snapshots/trades to determine current fill probability, except as a simulated post-placement fill process over a defined resting interval.

### Order lifecycle mismatch

Live passive Polymarket limit orders are GTC/GTD and remain open until filled/cancelled/expired. A paper model that immediately converts every signal into a position does not match the live lifecycle.

### Status/accounting mismatch

In pm-bot today:

- backtest unfilled trades are recorded with `filled=False`, zero P&L, zero cost;
- paper dry-run trades are inserted as `status='open'` positions immediately;
- exposure and daily-spent calculations include `open` rows;
- duplicates are blocked only for `status='open'`.

This means maker-fill modeling affects live-mode backtest but not paper-mode daemon accounting.

## Metrics Commonly Logged

External guides commonly log these per trade:

- signal timestamp
- book timestamp
- book lag
- side
- requested size
- filled size
- unfilled size
- fill ratio
- reference price
- average fill price
- slippage bps
- blocked/rejected reason

Aggregate metrics:

- filled count
- skipped/unfilled count
- blocked trade count
- median slippage bps
- P90 slippage bps
- fill ratio by size bucket
- slippage/fill ratio by market regime or event window
- gross P&L vs execution-adjusted net P&L

pm-bot currently logs only aggregate fill/skip counts for live-mode backtest and does not store fill probability or skipped dry-run orders in paper mode.

## Mapping to pm-bot Existing Components

### `FillModel`

Current role:

- Stateless probability function used by backtest only.
- Price-only input.
- Tail-aware, but not quote-context-aware.

Natural mapping points:

- Shared utility for both backtest and paper daemon.
- Deterministic sampling through a seeded RNG in backtests and optionally stable dry-run seeding.
- Potential future extension point for quote-context inputs if best bid/ask/spread become available.

### `BacktestEngine`

Current role:

- Applies maker Bernoulli sampling in live mode.
- Stores unfilled simulated trades as `filled=False`.
- Uses `CostModel` after fill succeeds.

Mapping notes:

- Existing behavior already models all-or-nothing maker fills.
- Existing behavior is compatible with a paper-mode model that records skipped/unfilled outcomes separately from filled/open positions.
- Current model does not perform expected-P&L weighting despite archived notes mentioning expected P&L by fill probability; actual code samples a Bernoulli event and skips on failure.

### `PaperTradeDB`

Current role:

- Persists paper positions/trades, not explicit order lifecycle events.
- Inserts every `record_trade` as `status='open'`.
- Settles only open positions.

Mapping notes:

- Paper fill modeling needs a representation for unfilled/skipped/pending orders if it is to preserve backtest-like transparency.
- If paper mode only inserts filled orders, skipped orders would need logging elsewhere to avoid invisible selection bias.
- If paper mode inserts unfilled orders, duplicate/exposure/settlement queries must distinguish unfilled orders from open positions.

### `TradingDaemon._execute_trade`

Current role:

- Dry-run path converts a recommendation directly into a paper open position.
- Live path posts a GTC limit order and records it in `TradeDB` if the API call succeeds.

Mapping notes:

- The dry-run path is the direct place where paper fill sampling would align with backtest behavior.
- The live path already has order-status APIs available through `ClobTrader`; fill modeling is less relevant to real live execution than to dry-run simulation.
- Dry-run sampling should occur before `PaperTradeDB.record_trade` if unfilled orders are not meant to count as exposure.

### `ClobTrader`

Current role:

- GTC limit orders model passive maker behavior.
- FOK market buy models aggressive all-or-none behavior.
- Open-order and order-status methods support lifecycle reconciliation.

Mapping notes:

- Polymarket docs' order-type semantics match the wrapper's current method choices.
- For paper fidelity, GTC-like dry-run orders need an expiry/cancel convention if not immediately sampled as filled/unfilled.

## Related Spec / Task Documents

- `.trellis/tasks/05-06-live-backtest-fidelity/prd.md` — states that paper trading currently counts every signal as filled and requires paper trading to use a comparable fill model.
- `.trellis/tasks/archive/2026-05/05-05-05-05-backtest-fidelity/prd.md` — archived note that introduced `FillModel` defaults: 50% at best, 25% inside, 10% tail, Bernoulli sampling in live mode, and fill/skip counts.

## Not Found

- No `pm_bot/backtest/fill_model.py` file currently exists, despite being listed as an expected key file in the task PRD.
- No current paper-mode use of `FillModel` was found.
- No current paper schema columns for requested size vs filled size, fill probability, unfilled reason, partial fill quantity, order expiry, or order lifecycle state were found.
- No current implementation was found that uses `FillModel.fill_prob_inside`.
- No current implementation was found for L2 orderbook depth walking in pm-bot backtests.
