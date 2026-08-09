# PRD: Near-Certain Bond Strategy + Staged Entry + Cost Constraint

## Background

Weather bot 已有 5 个策略（gopfan2, laddering, tail_no_barbell, forecast_arb, resolution_delay）。社区验证的 3 个关键机制缺失：

1. **Near-Certain Bond** — 买 95-99¢ 确定性 YES，赚 1-5¢ daily yield
2. **Staged Entry** — 分步入场（48h 30% → 24h 60% → 8h 100%）
3. **Ladder Total Cost Constraint** — 阶梯总成本 < 90¢ 保证正 ROI

## Requirements

### R1: Near-Certain Bond Strategy (`near_certain_bond.py`)

新增策略类 `NearCeraldBondStrategy`，继承 `Strategy`：

- **触发条件**: bucket 的 YES price ≥ 0.95 且 ≤ 0.99
- **模型验证**: `bucket_probability_numpy` 返回概率 ≥ 0.98（模型确认几乎确定）
- **方向**: 买 YES
- **Edge 计算**: `edge = model_prob - yes_price`（通常 1-5¢）
- **仓位**: 使用 Kelly 公式，但 `max_position_usd` 提高到 $5（低风险策略允许更大仓位）
- **Kelly fraction**: 0.5（比其他策略更激进，因为确定性高）
- **Max per event**: 3 个 bucket（选 edge 最高的）
- **配置参数**:
  - `min_yes_price`: 0.95
  - `max_yes_price`: 0.99
  - `min_model_prob`: 0.98
  - `kelly_fraction`: 0.50
  - `max_position_usd`: 5.0

### R2: Staged Entry Module (`core/staged_entry.py`)

独立模块，不绑定特定策略，所有策略可调用：

- **函数**: `get_position_multiplier(hours_to_resolution: float) -> float`
  - `> 48h`: 返回 0.0（不入场）
  - `48-24h`: 返回 0.3（30% 仓位）
  - `24-8h`: 返回 0.6（60% 仓位）
  - `< 8h`: 返回 1.0（满仓）
- **函数**: `apply_staged_entry(recs: list[Recommendation], hours_to_resolution: float) -> list[Recommendation]`
  - 对每个 Recommendation 的 `size_usd` 乘以 multiplier
  - 过滤掉 multiplier = 0 的
- **配置**: 可通过 config 调整各阶段比例和时间窗口

### R3: Ladder Total Cost Constraint

修改 `LadderingStrategy.run()`:

- 在选完 candidates 后，计算总成本 `total_cost = sum(b.yes_price for b in selected_buckets)`
- 如果 `total_cost > max_ladder_cost`（默认 0.90），逐步移除最贵的 bucket 直到满足约束
- 新增参数 `max_ladder_cost: float = 0.90`
- 在 reasoning 中标注 `LADDER COST={total_cost:.2f}`

### R4: Integration

- `strategies/__init__.py` 注册新策略
- `strategies/base.py` 的 `get_all_strategies()` 添加 `near_certain_bond`
- `models/config.py` 的 `STRATEGY_DEFAULTS` 添加 `near_certain_bond` 默认值
- CLI `pm-bot scan` 支持 `--strategy near_certain_bond`

### R5: Tests

- 单元测试: `tests/test_near_certain_bond.py`
  - 测试 bucket 筛选（价格范围、模型概率）
  - 测试 Kelly 仓位计算
  - 测试 staged entry multiplier
  - 测试 ladder cost constraint
- 集成测试: 确保新策略能被 `get_all_strategies()` 发现

## Out of Scope

- Parlay 支持（需要跨市场组合，复杂度高，后续单独做）
- 城市波动率筛选（需要历史数据积累，后续做）
- Resolution source 自动匹配（已有 `station_bias.py`，本次不改）

## Acceptance Criteria

1. `NearCeraldBondStrategy` 能独立运行，输出合理 Recommendation
2. Staged entry 模块可被任何策略调用
3. Laddering 总成本 ≤ 90¢ 约束生效
4. 所有现有测试不 break
5. 新策略通过 lint + type-check
6. Backtest engine 支持新策略
