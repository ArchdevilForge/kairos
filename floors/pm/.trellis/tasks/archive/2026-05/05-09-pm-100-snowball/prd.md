# PRD: Polymarket $100 Aggressive Snowball — Strategy & Backtest Optimization

## Summary
全面优化 Polymarket 每日最高温度策略与回测引擎，专为 **$100 USDC 初始资金** 设计。采用极高风险打法，通过高赔率长尾桶、laddering、resolution 边缘套利实现快速复利滚雪球。

## Motivation
现有引擎仅有一个活跃策略（gopfan2 tail-YES lottery），$100 资金下存在明显痛点：
- 仓位上限 10% Kelly × 10% max = 单笔仅 $1，增长极慢
- 缺少蒙特卡洛多路径模拟和破产概率分析
- 费用建模过于粗糙（固定 1% vs 实际动态费率）
- 没有 $100 → $500 → $2000 的雪球曲线追踪
- 策略库单一，缺少 laddering、resolution 延迟、预报套利等打法

## Scope
4 个 Phase，严格按序执行：
1. 项目审计 + $100 适配评估
2. 回测引擎 $100 专用强化
3. 策略筛选与高赌性优化
4. 最终交付与滚雪球路线图

## Technical Requirements
- Python 3.10+, 标准库 + numpy/scipy/pandas/plotly
- 遵循现有 Trellis spec 结构
- 所有回测标注「$100 起始资金」
- Monte Carlo 模拟 >= 1000 条路径
- 支持 12 个月历史数据回测

## $100 专用核心参数
- 初始资金：$100 USDC
- 单笔仓位：20-80%（极端允许 all-in）
- Kelly 分数：0.40-0.80（激进区）
- 费用模型：Weather taker 0.05 × p × (1-p)，极低桶几乎 0 费用
- Gas：$0.01/tx (Polygon)
- 目标雪球路径：$100 → $500 → $2000 → $10000

## Exit Criteria
- 可运行的优化引擎 + 3-5 个经过 $100 规模验证的高赌性策略
- 清晰的滚雪球路线图
- 完整的蒙特卡洛压力测试报告
