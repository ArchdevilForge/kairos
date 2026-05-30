---
name: bitlanglang-trading
description: Bit浪浪完整交易系统 - 基于pwatch CLI的合约交易agent技能
version: 1.0.0
author: pwatch
license: MIT
metadata:
  hermes:
    tags: [trading, crypto, futures, funding-arbitrage, technical-analysis]
    category: finance
    requires_toolsets: [code, web]
    requires_tools: [code_execution, web_search]
    config:
      - key: trading.default_exchange
        description: "Default exchange for trading"
        default: "okx"
        prompt: "Enter default exchange (binance, okx, bybit)"
      - key: trading.default_leverage
        description: "Default leverage for trades"
        default: 5
        prompt: "Enter default leverage (1-20)"
      - key: trading.execution_mode
        description: "Execution mode: signal (monitor only), semi-auto (confirm key ops), full-auto (autonomous)"
        default: "semi-auto"
        prompt: "Enter execution mode (signal, semi-auto, full-auto)"
      - key: trading.capital
        description: "Trading capital in USDT"
        default: 10000
        prompt: "Enter trading capital"
      - key: trading.risk_per_trade_pct
        description: "Risk per trade as percentage of capital"
        default: 33
        prompt: "Enter risk per trade percentage (1-100)"
---

# Bit浪浪交易系统 - Hermes Agent Skill

## 核心交易哲学

**顺势而为**：坚决不做逆势单，宁愿踏空绝不摸顶
**敬畏市场**：市场永远是对的，始终保持敬畏之心

## 市场周期判断（春夏秋冬理论）

使用量化指标判断当前市场阶段：

### 量化指标
| 指标 | 春天 | 夏天 | 秋天 | 冬天 |
|------|------|------|------|------|
| BTC 30日涨幅 | 10-30% | >30% | 高位震荡 | <-10% |
| 波动率 | 中等 | 高 | 低 | 高 |
| 成交量 | 增加 | 增加 | 减少 | 减少 |
| 资金费率 | 正常 | 高正 | 极高 | 负/极低 |
| 山寨币相关性 | 高 | 高 | 低 | 中等 |

### 周期策略
- **春天**：开始试仓，正常杠杆
- **夏天**：重仓出击，激进杠杆，聚焦龙头
- **秋天**：轻仓防守，收缩仓位，降低频率
- **冬天**：空仓等待，坚决管住手

## 选币策略

### 量化筛选条件
```
pwatch scan --exchange okx \
  --min-volume-24h 80000000 \
  --min-open-interest 25000000 \
  --min-listing-age 45 \
  --max-volatility 6.0
```

### 优先级
1. **次新币**：上市45-180天，上方无套牢盘
2. **热点板块**：AI、动物园系列、Meme
3. **与大盘共振**：与BTC同时启动
4. **抗跌性**：BTC回调时拒绝下跌

## 箱体识别

### 箱体定义
- **上沿**：多次触及的高点区域
- **下沿**：多次触及的低点区域
- **二次探顶/底**：箱体成熟标志
- **收敛**：波动范围缩小

### 使用CLI检测箱体
```bash
# 检测箱体模式
pwatch box-detect --symbol BTC/USDT --timeframe 15m --lookback 100

# 输出示例
# Box detected: BTC/USDT
# High: 68500, Low: 67200
# Status: CONVERGING
# Ready for breakout: YES
```

### 箱体交易策略
1. **箱体突破**：收敛充分后放量突破，止损放箱体下沿
2. **箱底承接**：价格触及箱体底部不破，做多止损放箱体最下沿
3. **规避中间**：箱体中间位置不做，盈亏比极差

## 进场策略

### 1. 趋势初期：大级别箱体突破
```bash
# 检测突破信号
pwatch signal --symbol BTC/USDT --strategy box_breakout
```

### 2. 趋势中期：分歧做承接
```bash
# 检测小分歧
pwatch signal --symbol BTC/USDT --strategy small_pullback

# 检测大分歧
pwatch signal --symbol BTC/USDT --strategy large_pullback
```

### 3. 右侧信号
- **不创新低**：下跌后反弹再跌不破前低
- **回踩企稳**：突破后回踩支撑位不破

## 仓位管理

### 固定仓位原则
```bash
# 查看当前仓位状态
pwatch position status

# 计算仓位大小
pwatch position size --capital 10000 --risk-pct 33 --leverage 5
```

### 杠杆限制
- **BTC**：≤10倍
- **山寨币**：≤5倍
- **套利**：≤3倍

### 分仓策略
- 总资金等份划分（如3份）
- 每次只用1份开仓
- 亏损从场外补，盈利提现

## 止损策略

### 结构止损
```bash
# 计算止损位
pwatch stop-loss --symbol BTC/USDT --entry 68000 --side long --strategy box_breakout

# 输出示例
# Stop loss: 67200 (box low)
# Risk: 800 (1.18%)
# Position size: 4150 USDT
```

### 止损类型
1. **箱体止损**：放在箱体最下沿
2. **拐点止损**：放在双底最低点下方
3. **成本止损**：行情脱离成本区后推到成本价

## 止盈策略

### 分批出货
```bash
# 计算止盈目标
pwatch take-profit --symbol BTC/USDT --entry 68000 --side long

# 输出示例
# TP1: 70000 (2.9%) - 30% position
# TP2: 72000 (5.9%) - 30% position
# TP3: 75000 (10.3%) - 40% position
```

### 出场信号
1. **全包回来**：大阴线吞没涨幅
2. **该突破不突破**：完美结构未能兑现
3. **丧失龙头地位**：资金转移到新龙头
4. **大盘见顶**：BTC到达波段顶部

## 资金费率套利

### 监控费率
```bash
# 查看所有费率
pwatch funding --exchange all

# 查看极端费率
pwatch funding --extreme --threshold 0.05

# 查看套利机会
pwatch funding --opportunities
```

### 套利执行
```bash
# 评估套利机会
pwatch arb evaluate --symbol BTC/USDT

# 执行套利（需确认）
pwatch arb execute --symbol BTC/USDT --size 1000

# 查看套利状态
pwatch arb status

# 关闭套利
pwatch arb close --id arb_BTC_1234567890
```

### 套利策略
1. **跨所套利**：A所正费率开空+B所开多
2. **费率+趋势**：费率信号结合趋势方向
3. **极端行情**：年化>50%时考虑介入

## 技术分析

### K线形态
```bash
# 检测K线形态
pwatch pattern --symbol BTC/USDT --timeframe 15m

# 输出示例
# Patterns detected:
# - Double bottom at 67200 (bullish)
# - Box breakout at 68500 (bullish)
```

### 支撑阻力
```bash
# 查看支撑阻力位
pwatch sr --symbol BTC/USDT

# 输出示例
# Resistance: 70000, 72000, 75000
# Support: 67200, 66000, 65000
# Current: 68000
```

## 复盘与学习

### 交易记录
```bash
# 查看交易历史
pwatch history --limit 50

# 查看策略统计
pwatch stats --strategy box_breakout

# 导出交易记录
pwatch history --export trades.csv
```

### 学习机制
1. **每笔交易后**：记录决策依据和结果
2. **每日复盘**：总结当日操作，识别模式
3. **每周优化**：调整参数，更新策略

## 风险控制

### 风险检查
```bash
# 检查风险状态
pwatch risk status

# 输出示例
# Capital: 10000 USDT
# Open positions: 2
# Total exposure: 6600 USDT (66%)
# Daily PnL: +150 USDT (1.5%)
# Consecutive losses: 0
```

### 风险限制
- 单笔最大仓位：33%
- 总暴露上限：66%
- 日亏损上限：10%
- 最大连续亏损：5次

## 执行模式

### Signal模式（纯监控）
```bash
pwatch config set execution_mode signal
```
- 只生成信号，不自动执行
- 适合学习阶段

### Semi-auto模式（半自动）
```bash
pwatch config set execution_mode semi-auto
```
- 自动分析，人工确认关键操作
- 推荐默认模式

### Full-auto模式（全自动）
```bash
pwatch config set execution_mode full-auto
```
- agent完全自主执行
- 仅异常时通知
- 适合成熟策略

## 常用命令速查

### 市场分析
```bash
pwatch cycle                          # 查看市场周期
pwatch scan                           # 扫描潜在标的
pwatch box-detect --symbol BTC/USDT   # 检测箱体
pwatch signal --symbol BTC/USDT       # 检测信号
pwatch sr --symbol BTC/USDT           # 支撑阻力
```

### 交易执行
```bash
pwatch position status                # 仓位状态
pwatch order --symbol BTC/USDT --side long --size 1000  # 下单
pwatch close --symbol BTC/USDT        # 平仓
pwatch stop-loss --symbol BTC/USDT    # 设置止损
```

### 资金费率
```bash
pwatch funding                        # 查看费率
pwatch funding --extreme              # 极端费率
pwatch arb status                     # 套利状态
pwatch arb execute --symbol BTC/USDT  # 执行套利
```

### 风险管理
```bash
pwatch risk status                    # 风险状态
pwatch history                        # 交易历史
pwatch stats                          # 策略统计
```

## 注意事项

1. **先用模拟盘**：测试策略后再用实盘
2. **严格执行止损**：止损是保命底线
3. **控制杠杆**：高倍杠杆是死路一条
4. **顺势而为**：坚决不做逆势单
5. **保持敬畏**：市场永远是对的

## 学习资源

- 完整交易系统文档：`references/trading-system.md`
- 箱体识别算法：`references/box-pattern.md`
- 仓位管理模板：`templates/position-sizing.md`
