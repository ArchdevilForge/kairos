---
id: H-005
status: proposed
source: Uniswap Liquidity Launchpad / CCA 官方文档 + 社区叙事(pools.trade 非官方)
created: 2026-08-09
---

# H-005: Launch Demand → Aftermarket Momentum

## 原始观点(Source)
"拍卖需求强的 launch,上币后延续概率高"(newsliquid 式数据驱动,未经验证)

## 可验证表述
Robinhood Chain 上 Uniswap CCA launch 的 demand 特征(unique_bidders /
top5 concentration / clearing_price/floor / final-20% bid growth)能否预测
aftermarket 1m/5m/15m/1h/4h 回报?

## 可观测数据
- 数据源: Robinhood Chain RPC + Uniswap Launchpad 合约事件(Auction/Migration/NewPool)
- 特征: floor_price, clearing_price, clearing/floor, total_raised, unique_bidders,
  top1/top5 bid share, bid_growth_1m/5m, clearing_price_velocity, auction_duration,
  token_supply, implied_FDV
- 结果: migration_price, initial_liquidity, return_1m/5m/15m/1h/4h, MFE, MAE

## 测试设计
- 模式: shadow(所有 launch 自动记录,先采数据)
- 判定: 按 demand 分位分组,比较 median return / PF;top 分位 vs 全体

## 结果
- 样本数: 0(采集器未上线)
- 结论: pending
