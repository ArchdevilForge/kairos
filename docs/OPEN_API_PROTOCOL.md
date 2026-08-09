# AiCoin Open Data API — 黑盒协议逆向报告

目标页: https://www.aicoin.com/zh-Hans/opendata  
文档: https://docs.aicoin.com  
签名说明: https://docs.aicoin.com/apis/generate-signature  

日期: 2026-07-25  
方法: 页面 HTML/JS 静态分析 + Playwright 网络观察 + 官方 docs 提取 + 无凭证主动探测  

---

## 1. 架构总览

存在 **两套 API 面**：

| 面 | Host | 用途 | 认证 |
|---|---|---|---|
| Open Data API | `open.aicoin.com` / `openapi.aicoin.com` | 对外数据接口 | HMAC 查询参数签名 |
| 站点管理 API | `www.aicoin.com` | 密钥/订单/用量 | 网站登录会话 |

```
[用户浏览器]
   |  cookie session
   v
www.aicoin.com  --POST /api/upgrade/openData/*-->  发密钥 / 下单 / 查日志
   |
   |  AccessKeyId + Secret (用户保存)
   v
open.aicoin.com / openapi.aicoin.com
   GET /api/v2/*  或  /api/upgrade/v2/*
   ?AccessKeyId&SignatureNonce&Timestamp&Signature&业务参数
```

前端: Next.js (`universal-web`)，静态资源 `webassets.aicoin.com`。

---

## 2. Open API 认证协议（完整）

### 2.1 传输

- 协议: HTTPS GET（业务接口基本全是 GET；POST 会 `405 Method Not Allow`）
- 认证字段位置: **Query String**（不是 Header）
- 业务参数: 同为 Query；**不参与签名**

### 2.2 公共认证参数

| Name | Type | 说明 |
|---|---|---|
| AccessKeyId | string | 访问密钥 ID |
| SignatureNonce | string | 随机串，每次请求应不同（示例用 8 位 hex） |
| Timestamp | string | Unix 秒级时间戳，**有效期 30 秒** |
| Signature | string | 见下 |

### 2.3 签名算法（已用官方 test vector 验证）

```
plain = "AccessKeyId={ak}&SignatureNonce={nonce}&Timestamp={ts}"
raw   = HMAC-SHA1(key=accessSecret, msg=plain)   # 20 bytes
hex   = lowercase_hex(raw)                       # 40 ascii chars
sig   = Base64(hex as ascii bytes)               # 注意: base64 的是 hex 字符串，不是 raw
```

**坑点**: 常见 HMAC 是 `Base64(raw)`；这里是 `Base64(hex(raw))`。  
官方 Go 示例与 JS quickstart 一致，本地复现 **full match**。

### 2.4 官方 Test Vector

```
AccessKeyId     = 975988f45090561684b7d8f4e45b85c2
accessSecret    = 957f23f2d6435e37d4ac21f3e9a67d45
SignatureNonce  = 2
Timestamp       = 1612149637
plain           = AccessKeyId=975988f45090561684b7d8f4e45b85c2&SignatureNonce=2&Timestamp=1612149637
HMAC-SHA1 hex   = 3f483ea5041b18924f0d16f5a32375579553403c
Signature       = M2Y0ODNlYTUwNDFiMTg5MjRmMGQxNmY1YTMyMzc1NTc5NTUzNDAzYw==
```

> 实测: 该 demo 密钥 **线上无效**（`验证失败` / `AccessKey无效`）。仅用于算法校验。

### 2.5 请求示例

```bash
# 伪代码
AK=... SK=...
NONCE=$(openssl rand -hex 4)
TS=$(date +%s)
PLAIN="AccessKeyId=${AK}&SignatureNonce=${NONCE}&Timestamp=${TS}"
HEX=$(printf %s "$PLAIN" | openssl dgst -sha1 -hmac "$SK" | awk '{print $2}')
SIG=$(printf %s "$HEX" | base64 -w0)

curl -G 'https://open.aicoin.com/api/v2/coin/ticker' \
  --data-urlencode "coin_list=bitcoin" \
  --data-urlencode "AccessKeyId=$AK" \
  --data-urlencode "SignatureNonce=$NONCE" \
  --data-urlencode "Timestamp=$TS" \
  --data-urlencode "Signature=$SIG"
```

Python 客户端: `proto/aicoin_open_client.py`

### 2.6 Base URL

| 类别 | Base |
|---|---|
| 综合数据 API | `https://open.aicoin.com/api/v2` |
| 高级数据 API | `https://open.aicoin.com/api/upgrade/v2` |
| 部分路径（文档示例） | `https://openapi.aicoin.com/api/upgrade/v2` |
| Hyperliquid | `https://open.aicoin.com/api/upgrade/v2/hl/...` |
| 分销机构 | `https://open.aicoin.com/api/upgrade/v2/distributor` |

端点清单见 `artifacts/endpoints.txt`（从 docs 抽出 ~148 条）。

### 2.7 错误形态（黑盒观察）

| 栈 | 无/错凭证响应 |
|---|---|
| `/api/v2/*` | HTTP 200 + `{"success":false,"errorCode":1001,"error":"验证失败"}` |
| `/api/upgrade/v2/*` | HTTP 401 + `{"success":false,"error":"缺少AccessKeyId参数"}` 或 `AccessKey无效` |
| 错误 method POST | `errorCode:405 Method Not Allow` |

两套后端错误模型不一致，像新旧网关并存。

---

## 3. 站点管理 API（密钥发放面）

Host: `https://www.aicoin.com`  
鉴权: **网站登录会话**（未登录统一）

```json
{"success":false,"message":"login required","status":403,"data":null}
```

| Method | Path | 作用 |
|---|---|---|
| POST | `/api/upgrade/openData/userApiKey` | 获取/重置 API Key；body `{}` 或 `{reset:1}` |
| POST | `/api/upgrade/openData/orderList` | 订单列表 |
| POST | `/api/upgrade/openData/order` | 创建订单（套餐购买） |
| POST | `/api/upgrade/openData/orderStatus` | 轮询支付状态 |
| POST | `/api/upgrade/openData/accessLog` | 调用日志；body `{startTime,endTime}` 秒级 |

前端约束: 调用前检查登录态，未登录 toast `loginRequired`。  
密钥 UI 字段: `accessKey` / `apiSecret`（opendata page chunk）。

**合法拿密钥路径**: 登录 www.aicoin.com → 开放数据页「立即使用/免费版」→ `userApiKey` 返回 ak/sk → 按 §2 签名调 open API。

---

## 4. 套餐 / 权限模型（页面提取）

| 套餐 | 价格 | 接口约数 | 频率 | 月请求 |
|---|---|---|---|---|
| 免费版 | $0 | ~15 | 15/min | 2万 |
| 基础版 | $29/mo | ~40 | 30/min | 2万 |
| 标准版 | $79/mo | ~60 | 80/min | 50万 |
| 高级版 | $299/mo | ~110 | 300/min | 150万 |
| 专业版 | $699/mo | 全部 | 1200/min | 350万 |

免费版覆盖: 部分市场/币种 + 空投/项目雷达。权限在服务端按 key 绑定，不是客户端可改字段。

---

## 5. 认证绕过探测结果

目标是 CTF 风格黑盒，对 open API 做了常见弱化探测。**未发现可直接无密钥调用业务数据的路径。**

| 探测 | 结果 |
|---|---|
| 完全无参数 | v2: 1001 验证失败；upgrade: 缺 AccessKeyId |
| 缺任意认证字段 | 仍失败 |
| 文档 demo ak/sk 实签 | 签名算法对，key 无效 |
| 弱 secret（空/全0/与ak相同） | 失败 |
| 过期 timestamp + 合法 demo 签名 | 失败 |
| Header 传 ak / Bearer / HMAC | 忽略，仍 1001 |
| POST body 塞认证 | 405 |
| 把会话 cookie 带到 open.aicoin.com | 无会话体系，无效 |
| 管理接口无登录 | 403 login required |
| 业务参数未签名 | **设计如此**；可篡改业务参数但必须先有有效签名 |

### 设计层面观察（非 exploit）

1. **签名不绑定 path/method/业务参数** — 经典“只签时钟+nonce”模型。持有 sk 后任意改 `coin_list` 等无需重算以外字段；对窃取 sk 后的滥用更方便，但不是未授权入口。  
2. **Timestamp 仅 30s** — 重放窗口短。  
3. **Nonce 文档要求每次不同** — 是否服务端强制 uniqueness 未在无密钥条件下验证。  
4. **demo 密钥写进公开 docs** — 信息泄露但已吊销/无效。  
5. **openapi vs open 双 host** — 部分路径 SSL/路由行为略有差异。

结论: **绕过认证使用接口 — 当前黑盒未打通。** 协议本身已完整还原；要调用需有效 AccessKey（免费登录可申请）。

---

## 6. 前端关键资产

| 资产 | 说明 |
|---|---|
| `/zh-Hans/opendata` page chunk | `page-b9a42c3b6fe83184.js` — 套餐 UI、userApiKey/order/accessLog |
| `1221-*.js` | 下单 `openData/order` / `orderStatus` |
| docs HTML | 全量 REST path + 签名多语言示例 |
| `a-new.aicoin.com` | 分析/统计像素（Matomo-like siteId），非 open API |

---

## 7. 本地复现

```bash
# 算法自检（不需要真密钥）
python3 proto/aicoin_open_client.py selfcheck

# 真密钥调用
export AICOIN_AK=... AICOIN_SK=...
python3 proto/aicoin_open_client.py ticker bitcoin
```

---

## 8. 未做 / 需要账号才能继续

- 登录后抓 `userApiKey` 真实响应字段全集  
- 用免费 key 枚举哪些 path 在 free tier 放开  
- Nonce 重放是否拒绝  
- 分销机构 API 额外权限模型  
- jshook 运行时 hook（协议已从 docs 明文拿到，hook 收益低）

---

## 9. 一句话协议规格

> **GET** `https://open.aicoin.com/api/{v2|upgrade/v2}/...`  
> Query 必带 `AccessKeyId, SignatureNonce, Timestamp, Signature`  
> `Signature = Base64( hex( HMAC-SHA1(secret, "AccessKeyId=...&SignatureNonce=...&Timestamp=...") ) )`  
> Timestamp 窗口 30s；业务参数同 Query 且不签名。
