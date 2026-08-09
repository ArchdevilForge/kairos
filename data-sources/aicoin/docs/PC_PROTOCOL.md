# AiCoin Windows 客户端协议逆向 (v2.16.10)

来源: 官网下载 `aicoinV2.16.10.exe` (NSIS) → `app-64.7z` → Electron `app.asar`  
**未依赖 Wine 运行**——静态拆包足够还原完整请求协议。  
可复现客户端: `aicoin_pc_client.py`

---

## 1. 包结构

```
aicoinV2.16.10.exe          # NSIS, ProductVersion 2.16.10
└─ $PLUGINSDIR/app-64.7z
   └─ AiCoin.exe            # Electron
      resources/app.asar    # 主逻辑 (~190MB)
      resources/app.asar.unpacked/
```

`package.json` 关键依赖:

| 包 | 作用 |
|---|---|
| `@aicoin/cryptaddon` | 原生 `.node`：RSA 密钥仓 + 交易所 API secret 加解密 (mbedtls) |
| `@aicoin/data_server_addon` | 行情子进程：聚合交易所 WS + `ws://stream.pc.aicoin.com:8080` |
| `better-sqlite3-multiple-ciphers` | 本地加密库 |
| `keytar` | OS 密钥链 |

`main`: `dist/main/main.js` (webpack + 字符串数组混淆)

---

## 2. 服务端入口

| 名称 | Base |
|---|---|
| API_BASE | `https://apipc.aicoin.com/api/` |
| VIP_API_BASE | `https://vip-pcapi.aicoin.com/api/` |
| TRADE_API_BASE | `https://trade.aicoin.com/api/` |
| TRADE_WS | `wss://bws.aicoin.com/ws` |
| QUOTE_WS | `ws://stream.pc.aicoin.com:8080` |
| IM | `https://im-socket.aicoin.com` / `im.aicoin.com` |
| ACE | `http://ace-svc.aicoin.com/api/` |

路径清单: `../artifacts/client_api_paths.txt` (~529 条，含 AiCoin 业务 path + 各交易所代理 path)

---

## 3. 业务 API 加密信封（核心）

### 3.1 明文请求体（拦截器注入）

```json
{
  "lan": "cn",
  "pc_client": "windows",
  "pc_client_version": "2.16.10",
  "token": "<登录后>",
  "...业务字段"
}
```

- `skipCrypt: true` 时不加密（如 `conn/load`）
- POST/PUT/DELETE/PATCH → 加密 `data`
- GET → 加密 `params`

### 3.2 密钥材料 (CryptoJS WordArray 语义)

```
key_raw = random_bytes(16)
iv_raw  = random_bytes(8)
key_hex = hex(key_raw)          # 32 ascii hex chars  = WordArray.toString()
iv_hex  = hex(iv_raw)           # 16 ascii hex chars

AES_key = UTF8(key_hex)         # 32 bytes → AES-256
AES_iv  = iv_raw || 0x00*8      # CryptoJS 8-byte IV，实现上按 16 字节零扩展可互通
```

### 3.3 加密

```
p  = Base64( AES-256-CBC-PKCS7( JSON_UTF8(body), AES_key, AES_iv ) )
k  = Base64( RSA-PKCS1v1.5_Encrypt( UTF8(key_hex), server_pubkey ) )
iv = Base64( RSA-PKCS1v1.5_Encrypt( UTF8(iv_hex),  server_pubkey ) )
v  = public_key_version          # 来自 conn/load，如 "1744770600"
```

请求:

```http
POST /api/<path> HTTP/1.1
Host: apipc.aicoin.com
Content-Type: application/json
compress: 1
User-Agent: AiCoin/2.16.10

{"p":"...","k":"...","v":"1744770600","iv":"..."}
```

### 3.4 公钥下发（无登录）

```http
POST /api/conn/load
Content-Type: application/json
compress: 0

{}
```

```json
{
  "success": true,
  "status": 200,
  "data": {
    "v": "1744770600",
    "info": "-----BEGIN PUBLIC KEY-----\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEA...\n-----END PUBLIC KEY-----",
    "renew": true
  }
}
```

注意: body 里 `v` 必须是 **字符串** 类型的版本号时才按“检查更新”逻辑；`{}` 直接返回当前钥。

### 3.5 响应解密

响应头: `Decompress: 1`

```
cipher = response.json.data          # base64 string
pt     = AES-256-CBC-PKCS7_Decrypt(cipher, AES_key, AES_iv)
# pt 为 gzip 字节 (1f 8b ...)
data   = JSON.parse( gunzip(pt) )
```

客户端等价路径: AES → WordArray.toString()(hex) → hexdecode → pako.ungzip → JSON.parse

### 3.6 服务器时间（无登录）

```
GET/POST https://apipc.aicoin.com/api/server/timestamp-plain
→ {"data": 1784965065}
```

---

## 4. 登录与 token

### 4.1 密码登录（已实测）

```
POST account/login   # 走加密信封
{
  "account": "email或手机",
  "pwd": "明文密码",          // 字段名是 pwd 不是 password
  "code": "",                 // 图形验证码，一般可空
  "os_info": "win32 x64",     // platform + ' ' + arch
  "2fa_code": "",
  "captcha_key": "",
  "version_info": "2.16.10",
  "user_client_id": "<machineId前20>",
  "device_name": "AiCoin PC (Win)"
}
```

成功响应 `data`:
```json
{
  "token": "...",
  "uid": 5843883,
  "nick_name": "...",
  "email": "...",
  "email_state": 1,
  "2fa_status": 0,
  "pro_end_time": 0,
  "qt_end_time": 0,
  "signal_end_time": 0,
  "avatar": "https://..."
}
```

- 之后每个加密请求自动带 `token` 字段
- 过期: `403` + `login required` / `登录信息已过期`
- 第三方: `upgrade/login/{google,apple,telegram,twitter}`
- 注册: `upgrade/user/register` `{email,password,code,user_client_id}`
- 验证码图: `GET account/imagecode`

### 4.2 已验证无登录可用

| path | 说明 |
|---|---|
| `conn/load` | RSA 公钥 |
| `server/timestamp-plain` | 服务器时间 |
| `upgrade/geoip` | IP/地区 |
| `upgrade/common/check-maintain` | 维护/下载配置 |

### 4.3 登录后已打通示例

| path | 说明 |
|---|---|
| `upgrade/market/tabList` | 行情 tab |
| `upgrade/bottom/hotCoins` | 热币 |
| `upgrade/bottom/data` | 底部数据 |
| `upgrade/search/coin` `{"search":"btc"}` | 币种搜索 |
| `upgrade/search/market` | 交易对搜索 |
| `upgrade/search/getMultiple` | 综合搜索热词 |
| `v3/newsflash/tab` / `list` | 快讯 |
| `v3/market/search` | 市场搜索 |
| `upgrade/billboard/getMarketHot` | 热榜 |
| `upgrade/hotList/index` | 热榜 index |
| `upgrade/whale/*_post` | HL 鲸鱼/持仓 |
| `upgrade/vip/getVipAbilityLimit` | VIP 能力 |
| `upgrade/user/service/level` | 用户等级 |
| `upgrade/assetAuth/getApis` | 资产 API 授权列表 |
| `upgrade/alert/config` | 预警配置 |
| `upgrade/customIndicator/script/list` | 自定义指标 |

---

## 5. cryptaddon（交易所密钥，非 HTTP 信封）

Windows native: `Aicoin_Crypt_Addon.node` + mbedtls RSA

JS opcode 协议 (Buffer):

| op | 名 | 用途 |
|---|---|---|
| 0x01 | GetVersion | 版本 |
| 0x02 | GetMD5KEY | 基于 hostname 派生 |
| 0x03 | GetCond | version+md5Key |
| 0x04 | InitKey | timestamp+version+md5Key → 会话 |
| 0x05 | EncMsg | RSA 加密 |
| 0x06 | ReWriteKey | 回填私钥 |
| 0x07 | DecMsg | RSA 解密 |
| 0x08 | SetUserID | 绑定 uid + userData 路径 |

上层还有 **TripleDES(aiKey)** 包一层，用于交易所 `secretKey` 本地存储。  
HTTP 业务信封 **不经过** cryptaddon，只用 JS RSA+AES。

---

## 6. 行情 data_server_addon

子进程 `childprocess/nodeaddon.js` → `addon.start(command)`

- 主备源: 币安/OKX/Bybit/Huobi/Bitget/Gate **直连** + AiCoin `ws://stream.pc.aicoin.com:8080`
- 深度不稳时回退 AiCoin 订阅 (`sub_aicoin_depth` / trades / mrtrades)
- HTTP 快照合并 depth

---

## 7. 与 OpenAPI 的关系

| | Open Data (`open.aicoin.com`) | PC 客户端 (`apipc.aicoin.com`) |
|---|---|---|
| 受众 | 第三方开发者套餐 | 官方桌面 App |
| 认证 | AccessKeyId + HMAC-SHA1-hex-Base64 | 登录 token + RSA/AES 信封 |
| 加密 | 无 body 加密 | 全量 AES+RSA |
| 重叠 | 文档化 REST 数据 | 全量产品功能 API |

两套完全独立。客户端 **不能** 直接当 open.aicoin.com 的 AccessKey 用。

---

## 8. 复现

```bash
python3 client/proto/aicoin_pc_client.py selfcheck
python3 client/proto/aicoin_pc_client.py call upgrade/geoip '{}'
python3 client/proto/aicoin_pc_client.py call upgrade/common/check-maintain '{}'

# 登录后:
AICOIN_TOKEN=xxx python3 client/proto/aicoin_pc_client.py call v3/newsflash/tab '{}'
```

---

## 9. 产物索引

```
client/
  dl/aicoinV2.16.10.exe
  app/                         # 解包 Electron
  asar/                        # app.asar 展开
  artifacts/
    client_api_paths.txt
    public_key.json
    urls.txt hosts.txt wss.txt
  proto/
    CLIENT_PROTOCOL.md         # 本文件
    aicoin_pc_client.py        # 可运行客户端
```

---

## 10. 结论

1. **完整 PC 协议已还原并可本地复现加密请求**。  
2. **绕过登录拿全量数据：未发现**——信封可伪造，但业务层校验 token。  
3. Wine 运行时抓包非必须；若要 token/登录流，再 Wine + 代理 / Frida 补会话即可。  
4. 合法路径: 正常登录客户端 → 导出 token → 本客户端重放。
