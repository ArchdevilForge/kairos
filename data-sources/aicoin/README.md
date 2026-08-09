# aicoin-api

AiCoin 接口工具包（逆向研究 / 个人数据接入）。

包含三套面：

| 模块 | Host | 认证 | 说明 |
|------|------|------|------|
| `aicoin_api.public` | `www.aicoin.com` | 无 | 站内 chart 公开接口（原仓库） |
| `aicoin_api.pc_client` | `apipc.aicoin.com` / `trade.aicoin.com` | RSA+AES 信封 + 登录 token | Windows 客户端协议 |
| `aicoin_api.open_client` | `open.aicoin.com` | AccessKey HMAC | 开放数据 OpenAPI |

> 不含安装包 / asar。协议文档与 **119+** 条已实测数据接口目录见 `docs/`。

## Install

```bash
pip install "git+ssh://git@github.com/ArchdevilForge/aicoin-api.git"
# pc/open 客户端依赖 cryptography
```

或直接拷贝 `aicoin_api/` 目录。

## 1) 公开 chart API（无鉴权）

```python
from aicoin_api import global_index, tp_detail, hot_news, search_key

for idx in global_index()[:3]:
    print(idx["index_name"], idx["last"])

print(tp_detail("btcusdt:binance")[0]["detail"]["en_name"])
```

## 2) PC 客户端协议（加密 + 登录）

```python
from aicoin_api.pc_client import login, Session

s, user = login("you@mail.com", "password")
print(user["uid"], user["nick_name"])

# 盘口
print(s.post("v2/transaction/index", {
    "symbol": "btcusdt:binance",
    "data_type": "bill",   # deal=成交
    "open_time": 24,
    "currency": "cny",
    "size": 30,
}))

# 搜索 / 快讯 / 鲸鱼
print(s.post("upgrade/search/coin", {"search": "btc"})["data"]["count"])
print(s.post("v3/newsflash/list", {"page": 1, "pagesize": 5, "tab": 0})["data"]["list"][0]["content"][:80])
print(s.post("upgrade/whale/market/overview_post", {})["data"])
```

CLI：

```bash
python -m aicoin_api.pc_client selfcheck
python -m aicoin_api.pc_client login <account> <password>
AICOIN_TOKEN=... python -m aicoin_api.pc_client call upgrade/bottom/hotCoins '{}'
```

### 信封算法（摘要）

```
POST https://apipc.aicoin.com/api/<path>
Header: compress: 1

key_hex = random(16).hex()          # AES-256 key = UTF8(key_hex)
iv_raw  = random(8); AES_iv = iv_raw || 0x00*8
p  = Base64(AES-CBC-PKCS7(json_body))
k  = Base64(RSA-PKCS1v1.5(key_hex))   # pubkey from POST conn/load
iv = Base64(RSA-PKCS1v1.5(iv_hex))
body = {p,k,v,iv}

Response Decompress:1 → AES decrypt → gzip → JSON
```

登录：`POST account/login` 字段 `account` / `pwd`（明文密码字段名是 pwd）。

完整说明：[`docs/PC_PROTOCOL.md`](docs/PC_PROTOCOL.md)  
数据接口目录：[`docs/DATA_API_CATALOG.md`](docs/DATA_API_CATALOG.md)（**119** 条实测）

## 3) Open Data API（HMAC）

```python
from aicoin_api.open_client import AiCoinOpenClient, generate_signature

# 算法自检（docs test vector）
from aicoin_api.open_client import _selfcheck  # or: python -m aicoin_api.open_client selfcheck

c = AiCoinOpenClient(access_key_id="...", access_secret="...")
print(c.coin_ticker("bitcoin"))
```

签名：

```
plain = f"AccessKeyId={ak}&SignatureNonce={nonce}&Timestamp={ts}"
Signature = Base64( hex( HMAC-SHA1(secret, plain) ) )  # base64 的是 hex 字符串
```

详见 [`docs/OPEN_API_PROTOCOL.md`](docs/OPEN_API_PROTOCOL.md)。

## Docs

| 文件 | 内容 |
|------|------|
| `docs/PC_PROTOCOL.md` | 桌面端完整协议 |
| `docs/DATA_API_CATALOG.md` | 数据接口 body/返回（忽略账号会员） |
| `docs/DATA_API_CATALOG.json` | 机器可读 |
| `docs/OPEN_API_PROTOCOL.md` | OpenAPI HMAC |
| `docs/client_api_paths.txt` | 客户端路径清单 |
| `samples/` | 脱敏响应样本 |

## 注意

- 研究/个人用途；请遵守 AiCoin 服务条款与当地法律。
- **不要**提交 token / 密码；仓库已忽略 `session.json`。
- 主图 K 线主路径是本地 `data_server` + 交易所 WS / `ws://stream.pc.aicoin.com:8080`，不是单一 REST。
- `trade.aicoin.com` 上部分 conf 接口常用明文 JSON + token（`skipCrypt`）。

## License

MIT
