---
id: market-source
type: module
title: 桌面与实时告警行情数据源
status: active
---

**目的**　用户从设置选择 Binance、OKX 或跟随 YAML，图表和本 daemon 的实时告警使用一致的行情来源。

**边界**　选择写入桌面数据库。显式选择使用该交易所作为唯一实时交易所。跟随 YAML 时继承其 primary 与实时交易所列表。币安账户对账仍使用独立凭据。CoinGlass 等独立配置按原配置运行。

**不变量**　历史、实时 K 线与复盘按交易所隔离，迁移仅导入符合当前时间边界的旧记录。切换关闭旧订阅和检测器再启动新实例。切换期间 API 可短暂返回 503，监听地址和令牌保持稳定。初次使用默认跟随 YAML。自动复算只处理当前交易所的票据，未标注交易所的旧票据按 YAML primary 归属。

**决策**　见 [ADR 0002](../90-engineering/adr/0002-desk-market-source.md)。
