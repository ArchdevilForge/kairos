# Research: Stop-Loss and Position Lifecycle for Polymarket CLOB Trading

## Query

Research realistic stop-loss and position lifecycle modeling for binary prediction markets / Polymarket CLOB trading. Include how stop-loss should be represented for limit orders, paper trading, backtests, stale orders, settlement, and live execution.

## Scope

- Internal project code and Trellis task/spec context under `/home/xeron/Coding/pm`.
- Polymarket CLOB public documentation for order lifecycle, order types, order queries, WebSocket user channel, heartbeat, and market-maker quote management.
- General binary prediction-market backtesting references where they describe execution-realistic simulation, orderbooks, lifecycle events, settlement, and stop-loss/time exits.

## Files Found

| File Path | Description |
|---|---|
| `.trellis/tasks/05-06-live-backtest-fidelity/prd.md` | Active task states daemon/backtest mismatch: CLI exposes stop-loss, daemon/paper lacks exit management, paper records all signals as open positions, and stale/unfilled orders + settlement need coherent state transitions. |
| `pm_bot/cli/app.py` | CLI exposes `daemon start --stop-loss` and `backtest --stop-loss`; daemon option sets `PM_BOT_STOP_LOSS`, backtest passes stop-loss into `_run_backtest`. |
| `pm_bot/backtest/costs.py` | Contains `FillModel` and `CostModel.stop_loss_slippage()`. Fill probability is price-zone based; stop-loss slippage defaults to 3% of wager. |
| `pm_bot/backtest/engine.py` | Live-mode backtest uses Bernoulli maker fills and caps losing resolved P&L when `stop_loss_pct > 0`, but this is outcome-level rather than order-lifecycle-level. |
| `pm_bot/core/paper_trade.py` | Paper DB records every signal directly as `status='open'` position, tracks bankroll/daily spent, detects duplicates only among open positions, and settles open trades to `status='settled'`. |
| `pm_bot/core/clob.py` | Live CLOB wrapper supports GTC limit buy/sell, FOK market buy, cancel single/all, get open orders, get order status, get trades, heartbeat, redeemable positions, and settlement/redeem calls. |
| `pm_bot/core/db.py` | Live daemon DB tracks submitted orders in `trades` with `fill_status='open'|'filled'|'cancelled'`, a separate `positions` table, duplicate checks, and reconciliation against API open order IDs. |
| `pm_bot/cli/daemon.py` | Daemon polls order status, maps `filled/matched` to filled and `cancelled` to cancelled, cancels all live orders on graceful shutdown, auto-settles live and paper positions, and reconciles open orders at recovery. |
| `.trellis/tasks/archive/2026-05/05-04-pm-bot-phase4/research/backtesting-frameworks.md` | Earlier research says original strategies were one-shot entry → hold until resolution, making vectorized backtesting acceptable before intraday stop-loss/limit-order interaction existed. It also describes finite-lifespan weather-market settlement and orderbook-walk fill simulation if book data exists. |
| `.trellis/tasks/archive/2026-05/05-05-strategy-research-prune/research/weather-strategies.md` | Notes heartbeat implications: resting limit-order bots must maintain heartbeat; loss of heartbeat cancels open orders. |

## External Facts Found

### Polymarket order representation

Polymarket documents that all orders are expressed as limit orders. “Market orders” are marketable limit orders that execute against resting liquidity at the best available price. The supported time-in-force/order types are:

| Order Type | Documented Behavior | Lifecycle Implication |
|---|---|---|
| `GTC` | Good-Til-Cancelled; rests until filled or cancelled. | Passive entry/exit orders can remain live indefinitely unless explicitly cancelled or killed by heartbeat/session behavior. |
| `GTD` | Good-Til-Date; active until a Unix expiration timestamp. Docs note a one-minute security threshold: effective expiration `N` seconds from now should use `now + 60 + N`. | Natural representation for short-lived quotes and stale-order prevention before weather cutoff/resolution/catalysts. |
| `FOK` | Fill-Or-Kill; must fully fill immediately or cancel. | Natural representation for all-or-nothing aggressive exits, including a stop-loss liquidation that should not create residual resting exposure. |
| `FAK` | Fill-And-Kill; immediately fills available quantity and cancels the rest. | Natural representation for partial aggressive exits when liquidity may be thin and residual position remains. |

Documented order response statuses include `live` (resting), `matched` (matched immediately), `delayed`, and `unmatched`. Open order objects include `id`, `status`, `market`/condition ID, `asset_id`/token ID, `side`, `original_size`, `size_matched`, `price`, `outcome`, `order_type`, `associate_trades`, `expiration`, and `created_at`. Trade objects include execution `price`, `size`, `fee_rate_bps`, `status`, `match_time`, `last_update`, `trader_side`, and `maker_orders`.

### Polymarket order and trade lifecycle

The documented lifecycle is:

1. Create and EIP-712 sign order.
2. Submit signed order to CLOB operator.
3. Operator validates signature, balance/allowance, and tick size.
4. If marketable, order matches immediately; otherwise it rests.
5. Resting orders stay open until matched, cancelled, or expired for GTD.
6. Matched trades are submitted onchain.
7. Trade status progresses `MATCHED -> MINED -> CONFIRMED`, with possible `RETRYING`, or terminal `FAILED`.
8. The Exchange contract atomically transfers conditional tokens and pUSD.

For cancellation, Polymarket states that orders can be cancelled before they are matched; partial fills cannot be cancelled, only the unfilled remainder can be cancelled. Cancel responses include `canceled` and `not_canceled` IDs/reasons.

### WebSocket and heartbeat facts

The authenticated user WebSocket channel emits:

- `TRADE` events when a market order is matched, a user limit order is included in a trade, and when trade statuses change (`MATCHED`, `MINED`, `CONFIRMED`, `RETRYING`, `FAILED`).
- `order` events for `PLACEMENT`, `UPDATE` when some of the order is matched, and `CANCELLATION`.

The market-maker docs explicitly say to monitor fills through the WebSocket user channel. They also say to cancel stale quotes immediately when market conditions change, use GTD to auto-expire before known catalysts, and call `cancelAll()` as a kill switch on errors or position breaches.

Polymarket heartbeat docs state that if a valid heartbeat is not received within 10 seconds plus up to a 5-second buffer, all open orders are automatically cancelled. The current project has a heartbeat loop in `pm_bot/core/clob.py` that posts every 5 seconds.

## Code Pattern Analysis

### Current backtest stop-loss model

`pm_bot/backtest/engine.py` applies stop-loss after settlement/outcome P&L is known:

```python
if self.stop_loss_pct > 0 and raw_pnl < 0:
    max_investment = size * (no_price if rec.direction == "NO" else effective_price)
    max_loss = max_investment * self.stop_loss_pct
    if abs(raw_pnl) > max_loss:
        slippage = self.costs.stop_loss_slippage(max_investment)
        pnl = -max_loss - cost - slippage
```

This appears in both the single-strategy loop (`pm_bot/backtest/engine.py:382-387`) and portfolio loop (`pm_bot/backtest/engine.py:619-624`). The trade record stores `entry_price`, `stop_loss_pct`, `price_source`, and `filled`.

The fill model used in live-mode backtest is separate from stop-loss:

```python
if self.live_mode and side == "maker" and self.costs.fill_model is not None:
    fill_prob = self.costs.fill_model.fill_probability(effective_price)
    filled = self._rng.random() < fill_prob
```

This means current backtest represents stop-loss as a capped realized loss on resolved losing contracts, not as an intraday stop order triggered by a tradeable exit price.

### Current paper lifecycle

`pm_bot/core/paper_trade.py` creates only one table for paper positions/trades. `record_trade()` inserts a row without an order lifecycle stage; the default status is `open`:

```sql
status TEXT NOT NULL DEFAULT 'open'
```

It immediately increments `daily_spent` by `size_usd` after insert. `check_duplicate_order()` only checks same `market_id` and `side` with `status='open'`. Settlement selects rows with `status='open'` and updates them to `status='settled'` with `settled_pnl` and `settled_at`.

Observed paper states are therefore:

```text
open -> settled
```

There is no representation for submitted-but-unfilled, partially filled, stale/cancelled, stop-triggered, exit-submitted, exit-filled, or trade-confirmation-failed states in the paper DB.

### Current live lifecycle

`pm_bot/core/db.py` records submitted orders with `fill_status='open'` by default. `update_fill_status()` supports `filled` and `cancelled` timestamps. `get_daily_spent()`, `get_city_spent()`, and `get_total_exposure()` count `open`, `partial`, and `filled` statuses. `check_duplicate_order()` checks only `fill_status='open'`.

`pm_bot/cli/daemon.py:_poll_fills()` maps API order status to DB status:

```python
if fill_status in ("filled", "matched"):
    self.db.update_fill_status(order_id, "filled")
elif fill_status in ("cancelled",):
    self.db.update_fill_status(order_id, "cancelled")
```

`_recover_state()` fetches `trader.get_open_orders()` and calls `reconcile_open_orders(api_ids)`. In `pm_bot/core/db.py`, any DB open order absent from API open order IDs is marked `filled`, while API orders absent from DB are logged as orphaned. This is a coarse reconciliation because Polymarket open-order disappearance can also mean cancellation, expiration, FOK/FAK unfilled, heartbeat kill, or a fully matched trade that later fails; trade history is needed to distinguish.

### Current live CLOB capabilities

`pm_bot/core/clob.py` supports the primitives needed to model lifecycle:

- `place_limit_buy()` and `place_limit_sell()` submit GTC limit orders.
- `place_market_buy()` submits an FOK market buy with a worst-price limit.
- `cancel_order()` and `cancel_all_orders()` cancel resting orders.
- `get_open_orders()`, `get_order_status()`, and `get_trades()` query order/trade state.
- `get_redeemable_positions()` and `settle_resolved()` handle winning position redemption.
- `start_heartbeat()`/`stop_heartbeat()` maintain session liveness for resting orders.

The wrapper currently does not expose GTD expiration, post-only, FAK market exits, market sells, batch cancels, or cancel-by-market, although those operations exist in Polymarket docs/SDKs.

## Realistic Representation by Domain

### 1. Stop-loss for limit orders

A stop-loss is not a property of the initial resting limit order in Polymarket CLOB docs. It is a position-management rule external to the CLOB:

```text
entry order submitted -> entry filled/partially filled -> position exists -> monitor mark/exit price -> when trigger condition is met, submit exit order/cancel stale entry as needed
```

For a binary YES/NO token, the stop trigger is naturally expressed on the tradeable token price or mark price, not on final payout. If buying YES at entry price `p_entry`, a stop-loss fraction `s` of invested capital corresponds approximately to an exit trigger near:

```text
YES stop trigger = p_entry * (1 - s)
```

For buying NO, the economically held token is NO at `p_no_entry = 1 - p_yes_entry`. The analogous stop trigger is:

```text
NO stop trigger = p_no_entry * (1 - s)
```

If the system stores NO positions in terms of the YES market price, the same NO stop condition can be written as:

```text
YES price trigger against NO = 1 - p_no_entry * (1 - s)
```

When triggered, the CLOB action is an exit sell order for the held outcome token, not a modification of the original buy. The exit can be represented as:

- Aggressive FOK sell: all-or-nothing liquidation at a worst acceptable price.
- Aggressive FAK sell: liquidate available depth, leave residual position if not fully filled.
- Passive GTD/GTC sell: attempt maker exit but with risk that the stop does not execute.

Polymarket docs note that market orders use FOK/FAK and the `price` is a worst-price limit, so a realistic stop-loss needs both `trigger_price` and `exit_limit_price`/slippage tolerance. A stop trigger does not guarantee execution in thin books.

### 2. Stop-loss in paper trading

Paper trading needs two separate concepts that the current table combines:

```text
order lifecycle: submitted/live/partial/filled/cancelled/expired/rejected/failed
position lifecycle: open/exit_pending/exited/settled
```

A realistic paper lifecycle for a maker entry is:

```text
signal
  -> entry_order_submitted
  -> entry_live
  -> entry_partial or entry_filled or entry_cancelled/expired/rejected
  -> position_open only for filled shares
  -> stop_monitoring while market active
  -> exit_order_submitted when mark <= trigger
  -> exit_partial/exit_filled or exit_failed
  -> position_exited or residual_position_open
  -> settled/redeemed if still open through resolution
```

For paper mode, fills should be simulated consistently with `pm_bot/backtest/costs.py:FillModel` rather than every signal becoming a position. The paper record should distinguish unfilled/cancelled orders from filled exposure, because only filled exposure should affect bankroll, settlement P&L, and duplicate-position protection. Unfilled orders may reserve notional while live, but should not settle.

For stop-loss paper execution, the mark source can be current best bid for the held token, midpoint, or last trade. The most conservative executable proxy for exiting a long outcome token is best bid/depth, because a sell stop must hit bids. Without historical/orderbook data, paper stop-loss can use a deterministic approximation:

```text
exit_value = filled_shares * max(trigger_price - stop_loss_slippage, 0)
realized_pnl = exit_value - entry_cost - fees/costs
```

and should record that it was a simulated stop exit rather than final settlement.

### 3. Stop-loss in backtests

The current backtest caps final losing P&L at the stop amount. This is a low-fidelity approximation because it assumes an intraday exit existed whenever a contract ultimately lost. Realistic stop-loss modeling requires an intraday price path or at least a conservative trigger proxy:

```text
entry fill -> observe subsequent price path -> if tradeable exit price crosses stop trigger before resolution, execute exit model -> otherwise settle at terminal payout
```

With only entry price and final outcome, the stop trigger is not observable. The existing capped-loss model is therefore an assumption, not a directly simulated event. Higher-fidelity backtests need one of:

- Historical orderbook/trade replay with maker/taker semantics, like PredictionMarketBench-style episode replay using orderbooks, trades, lifecycle, settlement, open orders, positions, and cash/equity.
- Intraday price history from CLOB/Dune/third-party data; trigger on bid/mid/last crossing and model depth/slippage.
- A conservative scenario rule that only applies stop-loss when an observed price series crosses the trigger, and otherwise settles normally.

Backtest limit-order entries should remain separate from positions. Maker entries can be represented as Bernoulli fill, queue/depth simulation, or orderbook replay. Unfilled maker orders should produce no settlement P&L. Partially filled orders should settle or stop only the filled shares.

### 4. Stale orders

Polymarket docs and market-maker guidance describe stale-order handling as order management, not settlement:

- GTC rests until filled/cancelled; it can become stale if fair value, forecast, or cutoff conditions change.
- GTD auto-expires at a specified timestamp and is documented for quotes before known events/catalysts.
- Cancel endpoints support single, multiple, all, and market-specific cancellation.
- Heartbeat loss automatically cancels open orders.

For weather markets with known resolution/cutoff, stale lifecycle representation should include:

```text
entry_live -> stale_cancel_requested -> cancelled
entry_live -> expired (for GTD)
entry_live -> heartbeat_cancelled / missing_from_open_orders
```

If partially filled before cancellation/expiration, the unfilled remainder is cancelled/expired while the filled shares become or remain a position.

### 5. Settlement

Settlement has two layers:

1. Trade settlement: after match, Polymarket trade statuses progress through `MATCHED`, `MINED`, `CONFIRMED`, `RETRYING`, or terminal `FAILED`. Only confirmed fills should be treated as final filled exposure in live accounting.
2. Market resolution/redemption: after binary outcome resolution, winning conditional tokens pay $1 and losing tokens pay $0. The project’s `ClobTrader.get_redeemable_positions()` uses Gamma positions filtered by `redeemable=true`; `settle_resolved()` calls `PolyWeb3Service.redeem()` or `redeem_all()`.

For binary positions:

```text
YES long settlement P&L = shares * outcome_value - entry_cost
NO long settlement P&L = shares * no_outcome_value - entry_cost
```

where `YES outcome_value = 1` if YES wins else `0`; `NO outcome_value = 1` if NO wins else `0`. In multi-outcome neg-risk weather markets, a NO on one bucket wins when any other bucket is the resolved winner.

If a stop-loss exit fully closes a position before market resolution, it should not also be settled. If partially exited, only the residual shares settle.

### 6. Live execution

Live execution needs to reflect Polymarket’s distinct order and trade events:

```text
submit order
  -> order status live/matched/delayed/unmatched or rejected
  -> order updates with size_matched
  -> trade events MATCHED/MINED/CONFIRMED/FAILED
  -> confirmed filled shares update position
  -> stale/cancel/expire affects only remaining leaves quantity
  -> stop trigger submits an opposite-side exit order for confirmed/reserved position shares
  -> market resolution/redeem handles residual open positions
```

Live stop-loss should be represented as a daemon-side rule over confirmed positions and current executable prices, not as a CLOB-native stop order. The observable trigger should use market data for the held token, preferably best bid/depth for sell exits. Once triggered, the execution action is an opposite-side order with explicit time-in-force and worst acceptable price. FOK can leave the position unchanged if not fully filled; FAK can close partially; passive GTD/GTC can fail to exit before further price movement.

For live reconciliation, API open-order absence alone is ambiguous. It can mean filled, cancelled, expired, heartbeat-cancelled, FOK/FAK killed, or matched then failed. Polymarket trade history/user WebSocket fields (`tradeIDs`, `associate_trades`, trade status, `size_matched`, `last_update`, `trader_side`) are the available facts to disambiguate.

## Position Lifecycle State Model

A realistic state model for this project can be described as two linked records: `Order` and `Position`.

### Order states

```text
created
submitted
live
partial
filled
cancel_requested
cancelled
expired
rejected
failed
```

Relevant Polymarket fields:

- `orderID` / `id`
- `market` / condition ID
- `asset_id` / token ID
- `side`
- `price`
- `original_size`
- `size_matched`
- `order_type`
- `expiration`
- `created_at`
- `associate_trades`

### Trade/fill confirmation states

```text
matched
mined
confirmed
retrying
failed
```

Relevant Polymarket fields:

- `trade_id`
- `status`
- `price`
- `size`
- `match_time`
- `last_update`
- `transaction_hash`
- `trader_side`
- `maker_orders`

### Position states

```text
none
open
exit_pending
partially_exited
exited
settlement_pending
settled
redeemed
```

Minimum position fields:

- market/condition ID and token/outcome identity
- direction (`YES` or `NO`) and actual held token
- confirmed shares
- average entry price / cost basis
- stop-loss config (`stop_loss_pct`, trigger price, slippage/worst-price policy)
- realized P&L and unrealized mark
- residual shares
- linked entry order IDs and exit order IDs

## Related Spec Documents

- `.trellis/tasks/05-06-live-backtest-fidelity/prd.md` — active PRD requiring daemon/paper stop-loss, fill model parity, lifecycle handling for stale/unfilled orders, stop-loss exits, settlement, and duplicate protection.
- `.trellis/tasks/archive/2026-05/05-04-pm-bot-phase4/research/backtesting-frameworks.md` — earlier backtest framework research; explains why one-shot hold-to-resolution strategies originally did not need intraday stop-loss management.
- `.trellis/tasks/archive/2026-05/05-05-strategy-research-prune/research/weather-strategies.md` — heartbeat note for resting limit orders.
- `.trellis/tasks/archive/2026-05/00-bootstrap-guidelines/research/polymarket-clob-trading.md` — earlier CLOB integration research covering GTC/GTD/FOK order creation, open orders, trades, orderbook endpoints, and tick size/neg risk requirements.

## Not Found

- No existing paper-trading implementation of stop-loss was found.
- No live daemon stop-loss trigger/exit-management implementation was found.
- No current project code was found that stores paper order states separate from paper positions.
- No current `ClobTrader` method was found for GTD order expiration, post-only orders, FAK exits, market sells, batch cancel, or cancel-by-market, although Polymarket docs list these capabilities.
- No current project code was found that reconciles disappeared live open orders using trade history before deciding whether they were filled versus cancelled/expired/failed.
