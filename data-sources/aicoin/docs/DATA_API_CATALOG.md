# AiCoin 数据接口深挖目录（v2）

忽略账号/会员。默认 Host=`apipc.aicoin.com/api`；`trade`=`trade.aicoin.com/api`（多明文+token）。

**合计实测可用：119 条**

鉴权：AES+RSA 信封 + `token`（`aicoin_pc_client.py login`）。


## 搜索（11）

| path | host | body | 返回 |
|---|---|---|---|
| `upgrade/dex/searchKol` | apipc | `{"search": "btc", "period": "month"}` | dict[list] list[2] |
| `upgrade/market/search-key` | apipc | `{"key": "btc"}` | dict[key,list] |
| `upgrade/search/article` | apipc | `{"search": "btc", "page": 1, "pageSize": 5}` | dict[count,latestTime,list] |
| `upgrade/search/block` | apipc | `{"search": "btc"}` | dict[count,list] |
| `upgrade/search/coin` | apipc | `{"search": "btc", "pageSize": 10, "curPage": 1}` | dict[list,count] |
| `upgrade/search/getMultiple` | apipc | `{"search": "btc"}` | dict[hotWords,unusualActionCoins,hotMarkets,hotBlocks,newTradingPairs,tabLi |
| `upgrade/search/market` | apipc | `{"search": "btc"}` | dict[count,list] |
| `upgrade/search/newsflash` | apipc | `{"search": "btc"}` | dict[list] |
| `v2/market/search/hot` | apipc | `{}` | list[8] |
| `v2/market/search/support-market` | apipc | `{}` | dict[market] |
| `v3/market/search` | apipc | `{"key": "btc", "curr_page": 1, "page_size": 10, "currency": "cny"}` | dict[currency_key,list,total] |

## 榜单/热度（19）

| path | host | body | 返回 |
|---|---|---|---|
| `upgrade/billboard/getCoinNew` | apipc | `{}` | dict[list,count] |
| `upgrade/billboard/getCoinPosition` | apipc | `{}` | dict[list,count] |
| `upgrade/billboard/getCoinTrade` | apipc | `{"page": 1, "size": 20, "currency": "cny", "sortKey": "trade24H", "direction": "desc"}` | dict[list,count] |
| `upgrade/billboard/getCoinTurnover` | apipc | `{}` | dict[list,count] |
| `upgrade/billboard/getHotCoinHour` | apipc | `{}` | dict[list,count] |
| `upgrade/billboard/getHotFlashTodaySearch` | apipc | `{"page": 1, "pageSize": 10, "word": "btc", "period": "24h", "currency": "cny"}` | dict[list,count] |
| `upgrade/billboard/getMarketHot` | apipc | `{}` | dict[list] |
| `upgrade/billboard/getStareLiq` | apipc | `{"size": 20, "page": 1, "currency": "cny", "cycle": "24H", "keyWord": "", "customGroupIds": []}` | dict[list,count] list[20] |
| `upgrade/billboard/getTpBasis` | apipc | `{"size": 20, "page": 1, "currency": "cny", "keyWord": "", "customGroupIds": []}` | dict[list,count] list[20] |
| `upgrade/billboard/getTpRate` | apipc | `{"size": 20, "page": 1, "currency": "cny", "keyWord": "", "customGroupIds": []}` | dict[count,list] list[20] |
| `upgrade/billboard/getTpRateHot` | apipc | `{"size": 20, "page": 1, "currency": "cny"}` | dict[list] list[20] |
| `upgrade/billboard/getTweetsInterpret` | apipc | `{"word": "btc", "period": "24h", "currency": "cny", "lan": "cn", "handleType": 1}` | dict[interpret] |
| `upgrade/billboard/unusualAction` | apipc | `{}` | dict[list,count] |
| `upgrade/bottom/customList` | apipc | `{}` | dict[list] |
| `upgrade/bottom/data` | apipc | `{}` | dict[list,polling] |
| `upgrade/bottom/hotCoins` | apipc | `{}` | dict[list] |
| `upgrade/bottom/unusualAction` | apipc | `{}` | dict[list] |
| `upgrade/home/coinHeatHistory` | apipc | `{"coins": ["bitcoin"], "period": "1d"}` | dict[bitcoin] bitcoin[0] |
| `upgrade/hotList/index` | apipc | `{}` | dict[list] |

## 快讯/内容/空投（12）

| path | host | body | 返回 |
|---|---|---|---|
| `upgrade/airdrop/cryptorank/list` | apipc | `{"page": 1, "pageSize": 20}` | dict[list,count,page,pageSize,totalPages] list[20] |
| `upgrade/article/authorRank` | apipc | `{}` | dict[list] |
| `upgrade/article/tabList` | apipc | `{}` | dict[tbody] |
| `upgrade/newsflash/coinHeatTweets` | apipc | `{"coins": ["bitcoin"], "period": "1d"}` | dict[bitcoin] bitcoin[0] |
| `upgrade/newsflash/tags/list` | apipc | `{}` | dict[id,listName,description,updatedAt,memberCount,pushEnabled,tagList] |
| `v3/newsflash/index-history` | apipc | `{"period": "7d"}` | dict[index_line,btc_price_line] |
| `v3/newsflash/list` | apipc | `{"page": 1, "pagesize": 10, "tab": 0}` | dict[ad,isLive,list,recentlyMember] |
| `v3/newsflash/market` | apipc | `{}` | list[2] |
| `v3/newsflash/sentiment-index` | apipc | `{}` | dict[index,update_time] |
| `v3/newsflash/tab` | apipc | `{}` | list[17] |
| `v3/newsflash/top-flash` | apipc | `{}` | dict[top_list] top_list[3] |
| `v3/newsflash/unread-list` | apipc | `{}` | dict[isLive,list,recentlyMember] |

## 链上/鲸鱼/HL/DEX（7）

| path | host | body | 返回 |
|---|---|---|---|
| `upgrade/dex/chance/communityList` | apipc | `{"page": 1, "pageSize": 10, "period": "24h"}` | dict[list,total] list[8] |
| `upgrade/dex/smart/whaleSearch` | apipc | `{"search": "btc", "period": "24h"}` | dict[list] list[0] |
| `upgrade/hl/smart-money/address/0x6ba889db7f923622d3548f621ecc2054b80c1817/profile` | apipc | `{"coin": "BTC", "days": 7}` | dict[address,pnlCurve,summary] pnlCurve[8] |
| `upgrade/hl/smart-money/address/0x6ba889db7f923622d3548f621ecc2054b80c1817/trades` | apipc | `{"coin": "BTC", "days": 7}` | dict[trades] trades[2000] |
| `upgrade/whale/latest_dynamics_post` | apipc | `{"page": 1, "pageSize": 20}` | list[100] |
| `upgrade/whale/market/overview_post` | apipc | `{}` | dict[total_position_value,long_position_value,short_position_value,total_ma |
| `upgrade/whale/positions_post` | apipc | `{"page": 1, "pageSize": 20}` | dict[data,total,page,pageSize,totalPages] data[20] |

## K线/深度/成交（9）

| path | host | body | 返回 |
|---|---|---|---|
| `upgrade/kline/chart/multi/global-setting` | apipc | `{}` | dict[global_setting] |
| `upgrade/kline/chart/multi/layout-list` | apipc | `{}` | dict[list] list[1] |
| `upgrade/kline/chart/multi/tp-detail` | apipc | `{"symbols": ["btcusdt:binance"], "lan": "cn"}` | dict[list] list[1] |
| `upgrade/kline/estLiqMapHistory` | apipc | `{"dbKey": "btcswapusdt:binance", "leverage": "100", "cycle": "24h", "limit": 100, "start_time": "...` | dict[time_points] |
| `upgrade/kline/footPoint` | apipc | `{"dbKey": "btcswapusdt:binance", "since": "<ms>", "reach": "<ms>", "period": 15, "type": "1"}` | dict[data] data list of footpoint bars |
| `v2/transaction/coinTrade` | apipc | `{"key": "bitcoin", "open_time": 24, "currency": "cny"}` | dict[degree_5m,fundNetInflow,high24h,low24h,marketCap,symbol,trade,turnover |
| `v2/transaction/index` | apipc | `{"symbol": "btcusdt:binance", "data_type": "bill", "open_time": 24, "currency": "cny", "size": 30}` | dict[changeValue,coversion_price,degree,last_deal,platform_price,symbol] la |
| `v3/kline/agg-trade` | apipc | `{"symbol": "btcusdt:binance"}` | dict[list,mapping,request_state] |
| `v3/kline/indicator-data` | apipc | `{"indicator_key": ["ma"], "currency": "usd", "period": 15, "symbol": "btcusdt:binance"}` | dict[list] |

## 市场元数据/配置/自选（28）

| path | host | body | 返回 |
|---|---|---|---|
| `custom/group_list` | apipc | `{}` | dict[def_id,list] list[2] |
| `custom/market_list` | apipc | `{}` | dict[list] list[0] |
| `custom/platform-list` | apipc | `{}` | dict[hot_list,list] hot_list[9] list[31] |
| `market/coin/detail` | apipc | `{"key": "bitcoin"}` | dict[coin_url,coin_show,coin_logo,rank,sup_val,investor,tab_list,coin_name, |
| `market/detail` | apipc | `{"platform_list": ["binance", "okx"]}` | dict[binance,okx] |
| `upgrade/common/check-maintain` | apipc | `{}` | dict[download,maintain] |
| `upgrade/common/getTabConfig` | apipc | `{}` | dict[tabs] tabs[2] |
| `upgrade/dictionary/list` | apipc | `{"type": "hot", "lan": "cn"}` | dict[list,total] |
| `upgrade/dictionary/typeMapping` | apipc | `{}` | dict[list] |
| `upgrade/geoip` | apipc | `{}` | dict[allowContinue,country,ip,notice,pass,raw] |
| `upgrade/market/hotspotGroupList` | apipc | `{}` | list[1] |
| `upgrade/market/hotspotPairList` | apipc | `{"tabKey": "hot", "page": 1, "pageSize": 20, "currency": "cny", "lan": "cn"}` | dict[list,total] list[0] |
| `upgrade/market/indexMarketType` | apipc | `{}` | list[16] |
| `upgrade/market/maintainStatus` | apipc | `{}` | dict[] |
| `upgrade/market/tabDetailList` | apipc | `{"tabKey": "hot", "fields": []}` | dict[list] list[0] |
| `upgrade/market/tabList` | apipc | `{}` | dict[list] |
| `upgrade/market/tradingScheduleConfigs` | apipc | `{}` | dict[hkex,i:cad:lme,i:es:cmegroup,i:inx:sp,i:nq:nasdaq,i:xagusd:liffe,i:xau |
| `v1/market/index` | apipc | `{}` | list[473] |
| `v2/confByTrading` | trade | `{"tradings": ["btcusdt:binance"]}` | dict[] |
| `v2/custom/all-group-keys` | apipc | `{}` | dict[list] |
| `v2/custom/tradeareas-tradepairs` | apipc | `{"market_key": "binance", "page": 1, "pageSize": 20, "currency": "cny", "open_time": 24}` | dict[list,total] list[0] |
| `v2/loadRelateSymbol` | trade | `{"key": "btcusdt:binance"}` | dict[] |
| `v2/loadSymbolConf` | apipc | `{"symbols": ["btcusdt:binance"]}` | dict[conf] conf[0] |
| `v2/loadTradingConf` | apipc | `{"tradings": ["btcusdt:binance"]}` | dict[conf] conf[0] |
| `v2/serverHost` | apipc | `{}` | dict[ip_list,ttl,host_list] |
| `v2/syncTime` | apipc | `{}` | dict[time] |
| `v2/tradeMarketList` | apipc | `{}` | dict[markets,conf] |
| `v3/tradeMarketList` | trade | `{}` | dict[markets,pauseTrade,stopTrade] markets[0] |

## 套利/策略(只读)（13）

| path | host | body | 返回 |
|---|---|---|---|
| `quant/fee-arbitrage-filter` | apipc | `{}` | dict[coin,market,tutorial,classify] coin[9] market[6] classify[4] |
| `quant/fee-arbitrage-list` | apipc | `{}` | dict[list,other] |
| `quant/marginLoanRateLine` | apipc | `{"symbol": "btcusdt:binance"}` | dict[line] line[36] |
| `quant/spread-arbitrage-filter` | apipc | `{}` | dict[coin,type,market,tutorial] coin[10] type[3] market[6] |
| `quant/spread-arbitrage-list` | apipc | `{}` | dict[list] |
| `quant/swap-fee-detail` | apipc | `{"symbol": "btcswapusdt:binance"}` | dict[now_rate,next_rate,day_rate,3day_rate,7day_rate,30day_rate,rate_trend] |
| `quant/tool/strategyList` | apipc | `{}` | dict[vipMaxCount,max_st_count,remain_count,qt_end_timestamp,list] |
| `upgrade/arbi/guide` | apipc | `{}` | dict[type,month_profit,year_profit,actions,market,maxProfit,maxType,default |
| `upgrade/coinSelect/strategyList` | apipc | `{}` | dict[recommend,custom,isCondition] |
| `upgrade/quant/tool/getRecommendStrategy` | apipc | `{}` | list[3] |
| `upgrade/strategy/ad/getStrategyAd` | apipc | `{}` | dict[configList] |
| `upgrade/strategy/dca/signal/list` | apipc | `{}` | dict[list] list[6] |
| `upgrade/strategy/list` | apipc | `{}` | dict[contractGrid,feeArbitrage,multiDca,spotDCA,spotGrid] |

## 指标/信号/预警（17）

| path | host | body | 返回 |
|---|---|---|---|
| `upgrade/alert/assetAlertConfigV2` | apipc | `{}` | dict[id,isApp,isEmail,isPc,openChange,openTotal,uid] |
| `upgrade/alert/config` | apipc | `{}` | dict[alert,mode,private] alert[13] mode[5] |
| `upgrade/alert/list/big_trade` | apipc | `{}` | dict[body,last_time,last_id,ews_count,invalid_count] body[0] |
| `upgrade/alert/list/signal` | apipc | `{}` | dict[body,last_time,last_id,ews_count,invalid_count] body[0] |
| `upgrade/coinSelect/getIndicatorFilterData` | apipc | `{}` | dict[fields,filterData] |
| `upgrade/customIndicator/indicator/config` | apipc | `{}` | dict[] |
| `upgrade/customIndicator/screener/config` | apipc | `{}` | dict[supportMarkets,supportSupply] |
| `upgrade/customIndicator/script/functionLib` | apipc | `{}` | dict[list] |
| `upgrade/customIndicator/script/list` | apipc | `{}` | dict[customList,recentlyVisit] |
| `upgrade/customIndicator/script/template` | apipc | `{}` | dict[sourceList,visualList] |
| `upgrade/drawTrade/list` | apipc | `{"page": 1, "pageSize": 20}` | dict[list,pageCursor,total] list[0] |
| `upgrade/ieoTrade/list` | apipc | `{"page": 1, "pageSize": 20}` | dict[list,pageCursor,total] list[0] |
| `upgrade/signalAlert/getSignalGlobalData` | apipc | `{}` | dict[indicatorAmount,patternAmount,periodCount,signalAmount,supportIndicato |
| `upgrade/signalsManager/list` | apipc | `{}` | dict[lasttime,list,total] |
| `upgrade/warning/quotes-special/history` | apipc | `{"keys": "btc", "size": 20, "last_id": 0, "last_time": 0}` | dict[body,last_id,last_time] body[0] |
| `v1/warning/quotes/history` | apipc | `{"keys": "btc", "size": 20, "last_id": 0, "last_time": 0}` | dict[last_id,last_time,body] body[20] |
| `v1/warning/quotes/tag` | apipc | `{}` | dict[body,special] body[11] special[3] |

## 其它（3）

| path | host | body | 返回 |
|---|---|---|---|
| `upgrade/calendar/marks` | apipc | `{}` | list[0] |
| `upgrade/custom/dynamic-group-list` | apipc | `{}` | dict[list] list[0] |
| `upgrade/d/indicator/tags` | apipc | `{}` | dict[tagSet] |

## 本轮新挖到的高价值接口

| path | body | 说明 |
|---|---|---|
| `upgrade/market/homeTPUnusualAction` | `{}` | 异动列表，样本 ~7000+ |
| `upgrade/billboard/getStareLiq` | size/page/currency/cycle=24H | 盯盘爆仓榜 |
| `upgrade/billboard/getTpRate` | +customGroupIds | 资金费率榜 |
| `upgrade/billboard/getTpBasis` | +customGroupIds | 基差榜 |
| `upgrade/billboard/getTpRateHot` | size/page/currency | 热门费率 |
| `upgrade/hl/smart-money/address/{addr}/profile` | coin/days | HL 地址画像+PnL曲线 |
| `upgrade/hl/smart-money/address/{addr}/trades` | coin/days | 单地址成交（可到 2000 条） |
| `upgrade/airdrop/cryptorank/list` | page/pageSize | 空投列表 |
| `upgrade/dex/searchKol` | search/period | DEX KOL |
| `custom/group_list` / `platform-list` | `{}` | 自选分组/平台列表 |
| `quant/marginLoanRateLine` | symbol | 借贷利率曲线 |
| `quant/fee-arbitrage-filter` | `{}` | 费率套利筛选项 |
| `v1/warning/quotes/history` | keys/size/last_id/last_time | 行情预警历史 |
| `upgrade/home/coinHeatHistory` | coins:["bitcoin"], period:1d | 币热度 |
| `upgrade/newsflash/coinHeatTweets` | coins:[...], period:1d | 推文热度 |

## 仍难啃 / 未完全打通

- `upgrade/billboard/spot|futures`：JS 参数已对齐仍空响应（可能区域/网关）
- `upgrade/kline/obi`、depth-stat：参数组合未闭合
- 主图 K 线 REST：主路径是 **WS** `ws://stream.pc.aicoin.com:8080` + 交易所直连（`data_server_addon`）
- VIP WS `ws://ws-pc-vip.aicoin.com:8080`：帧为 **gzip JSON**，`ping→{"type":"pong"}`；完整订阅帧需继续 hook
- 部分 HL deep-scan / scriptCommunity 要更复杂业务态

## 调用

```bash
python3 client/proto/aicoin_pc_client.py login '<acc>' '<pwd>'
AICOIN_TOKEN=... python3 client/proto/aicoin_pc_client.py call upgrade/market/homeTPUnusualAction '{}'
AICOIN_TOKEN=... python3 client/proto/aicoin_pc_client.py call \
  'upgrade/hl/smart-money/address/0x.../trades' '{"coin":"BTC","days":7}'
```

样本：`client/artifacts/deep/` + `deep4/`  
JSON：`DATA_API_CATALOG.json`
