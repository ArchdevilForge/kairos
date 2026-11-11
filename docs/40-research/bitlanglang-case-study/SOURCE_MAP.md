# Bit浪浪复盘全集 — 来源映射（2026-11-09 建立）

用户给的 YouTube 播放列表 PLv62PVNSGzGD2614LkngWETWL-fZebdja（23 集 / 19.99h）无字幕且 VM 出口被 YouTube 机器人墙；
改走 Bilibili「中山公爵」同系列搬运（ThinkPad 家宽下载），按日期+时长双匹配，22/23 集命中，时长误差均 <1 分钟。

| # | 日期 | YouTube ID | YT时长 | Bilibili BV | B站时长 | 状态 |
|---|---|---|---|---|---|---|
| 01 | 20231028 | 83-jhmek0iQ | 87.0m | BV1Yv9ABeEmv | 86:57 | ✅ |
| 02 | 20231104 | yzui2wDt2V4 | 110.1m | BV1PiDKBDEnr | 110:06 | ✅ |
| 03 | 20231111 | x9LO8XJtB_U | 79.6m | BV1WiDKBSEgA | 79:36 | ✅ |
| 04 | 20231118 | cjZlnwfqMio | 97.8m | BV12oXyBqEwC | 97:49 | ✅ |
| 05 | 20231125 | 4R0rkCp8Lus | 77.5m | BV1piDKBSE9o | 77:32 | ✅ |
| 06 | 20231202 | jdzHpVy4W5w | 51.5m | BV1WiDKBSEnz | 51:29 | ✅ |
| 07 | 20231209 | YX24ETuTRCA | 66.4m | — | — | ✅ 已补（2026-11-10）：B 站无搬运，改走 YouTube 原版——ThinkPad 家宽 + 登录完整会话 cookie + bgutil PO Token + yt-dlp 外置 curl 下载音频（`audio/07-20231209-YX24ETuTRCA.m4a`），Colab 转写 1460 段/66.8min，笔记与 STRATEGY 增补完成 |
| 08 | 20231216 | jbIuuAEhiFI | 34.9m | BV1v9DKBMESX | 34:54 | ✅ |
| 09 | 20231223 | uLvIyev9S1k | 69.7m | BV1L9DKBMEjX | 69:40 | ✅ |
| 10 | 20231230 | h8GqV-RuM3E | 41.9m | BV1L9DKBMEJC | 30:39 | ✅ 已补齐（2026-11-10）：本集为两段独立录像——B 站拷贝 = 大群公开段（30:39，大群段结尾原话「去小群讲一下上周的单子」），YT 原版 = 小群交割段（42.3min，同 ep07 路线补下，`audio/10-20231230-h8GqV-RuM3E.m4a`，转写 822 段）。两段内容零重叠、首尾相接，笔记已合并（小群段补遗） |
| 11 | 20240106 | CtVaRglppH8 | 51.2m | BV1L9DKBMEuB | 51:09 | ✅ |
| 12 | 20240113 | 6m1QRcKK1kU | 55.1m | BV1R9DKBMEoz | 55:06 | ✅（另有大群回放版 BV1L9DKBMEFe 31:13 作补充） |
| 13 | 20240225 | _T9r-v3pDjE | 40.8m | BV1iQDKBzEkW | 40:48 | ✅ |
| 14 | 20240302 | Gd8jEknDfgA | 31.5m | BV1LQDKBzE6Y | 31:28 | ✅ |
| 15 | 20240323 | OJ0jNyRITv4 | 41.6m | BV1LQDKBzEqp | 41:33 | ✅ |
| 16 | 20240330 | LM9vYx6FWV8 | 51.8m | BV1LQDKBzEzf | 51:47 | ✅ |
| 17 | 20240421 | pZCiEM0CYcY | 53.9m | BV1iQDKBzE3v | 53:55 | ✅ 核心体系课（春夏秋冬+6大买点） |
| 18 | 20240504 | aiHkDlyDLkg | 33.5m | BV1iQDKBzETy | 33:26 | ✅ |
| 19 | 20240511 | hUbItHnF9Xw | 38.1m | BV1eoDKBKENr | 38:07 | ✅ |
| 20 | 20240518 | E8_jRSuspvQ | 36.1m | BV1eoDKBKEVt | 36:04 | ✅ |
| 21 | 20240525 | AGw_TjQWwas | 24.1m | BV1VoDKBKERw | 24:08 | ✅ |
| 22 | 20240601 | NbRTpKBwyUI | 20.0m | BV19oDKBKEb4 | 19:56 | ✅ |
| 23 | (短片) | LRqfJiuAado | 5.4m | — | — | ⏭️ 7u赚到3000w直播片段，非体系课，低优先 |
| 补 | 20240309 | （YT无） | — | BV1LQDKBzECb | 48:38 | 小群回放，补充材料 |

TODO：无未决缺口（2026-11-10 更新）。关键截图层（2026-11-10 午后补）：截图一律取 YouTube 原版（用户指定优先于 B 站），方法=ThinkPad 沙箱 Chrome 登录会话 + CDP 定点 seek + 裁剪视频元素截图（脚本 `bitlang/frames/shot.mjs`）；经验：个别视频冷启停滞需 URL 带 `&t=` 预热+点击/play 助推、seek 会触发中插广告需播完重 seek、集尾锚点多为 BGM/桌面帧应避开。95 张入 `frames/` 并嵌入笔记。ep07 与 ep10 小群段均已于 2026-11-10 经 YouTube 原版路线补齐，全集 23 集 + 2 补充转写与蒸馏完成。YouTube 下载方法学（供日后复用）：沙箱 Chrome 必须先真实打开一次目标视频页，会话 cookie 才完整（含 VISITOR_INFO1_LIVE / LOGIN_INFO，仅有 Google 登录 cookie 不够）；yt-dlp 需 yt-dlp-ejs + node 运行时解 n/sig 挑战；媒体段 403 需 bgutil PO Token 服务（内容绑定）；最终以 `--external-downloader curl` 落地（原生下载器对 DASH 格式报 403），googlevideo 限速约 32KB/s，大文件须脱离桥的 600s 超时以 nohup 后台跑 + `--continue` 断点续传。
