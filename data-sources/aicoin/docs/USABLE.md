# 当前可用接口（实测）

账号 uid=5843883 / 加密信封 + token

## 免登录

- `conn/load`
- `server/timestamp-plain`
- `upgrade/geoip`
- `upgrade/common/check-maintain`

## 需登录（token）

| path | 示例 body | 返回摘要 |
|---|---|---|
| `upgrade/user/service/level` | `{}` | keys=['isAuthorized', 'isTrade', 'level', 'tags'] |
| `upgrade/vip/getVipAbilityLimit` | `{}` | keys=['functionVipAbilityLimit', 'otherAbilityLimit', 'userVipAbilityLimit'] |
| `upgrade/market/tabList` | `{}` | keys=['list'] |
| `upgrade/market/tabDetailList` | `{"tab": "hot"}` | keys=['list'] |
| `upgrade/bottom/hotCoins` | `{}` | keys=['list'] |
| `upgrade/bottom/data` | `{}` | keys=['list', 'polling'] |
| `upgrade/bottom/unusualAction` | `{}` | keys=['list'] |
| `upgrade/search/coin` | `{"search": "btc"}` | keys=['list', 'count'] |
| `upgrade/search/market` | `{"search": "btc"}` | keys=['count', 'list'] |
| `upgrade/search/getMultiple` | `{"search": "btc"}` | keys=['hotWords', 'unusualActionCoins', 'hotMarkets', 'hotBlocks', 'newTradingPairs'] |
| `upgrade/billboard/getMarketHot` | `{}` | keys=['list'] |
| `upgrade/hotList/index` | `{}` | keys=['list'] |
| `upgrade/common/getTabConfig` | `{}` | keys=['tabs'] |
| `upgrade/calendar/marks` | `{}` | list[0] |
| `upgrade/geoip` | `{}` | keys=['allowContinue', 'country', 'ip', 'notice', 'pass'] |
| `upgrade/common/check-maintain` | `{}` | keys=['download', 'maintain'] |
| `v3/newsflash/tab` | `{}` | list[17] |
| `v3/newsflash/list` | `{"page": 1, "pageSize": 2, "tab": 0}` | keys=['ad', 'isLive', 'list', 'recentlyMember'] |
| `v3/market/search` | `{"keyword": "btc"}` | keys=['currency_key', 'list', 'total'] |
| `v2/market/search/hot` | `{}` | list[8] |
| `upgrade/whale/market/overview_post` | `{}` | keys=['total_position_value', 'long_position_value', 'short_position_value', 'total_margin_used', 'long_margin_used'] |
| `upgrade/whale/positions_post` | `{}` | keys=['data', 'total', 'page', 'pageSize', 'totalPages'] |
| `upgrade/whale/latest_dynamics_post` | `{}` | list[100] |
| `upgrade/assetAuth/getApis` | `{}` | keys=['authLimit', 'list', 'syncStatus'] |
| `upgrade/alert/config` | `{}` | keys=['alert', 'mode', 'private'] |
| `upgrade/signalAlert/getSignalGlobalData` | `{}` | keys=['indicatorAmount', 'patternAmount', 'periodCount', 'signalAmount', 'supportIndicatorKeys'] |
| `upgrade/customIndicator/script/list` | `{}` | keys=['customList', 'recentlyVisit'] |

**合计：免登 4 + 登录后 27 = 31 条实测可用**

> 客户端逆向出 ~350 业务 path，上表仅为已探测确认可用子集；其余多需特定参数/VIP/业务上下文。
