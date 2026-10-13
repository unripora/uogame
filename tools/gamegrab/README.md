# gamegrab — 通用 H5 抓取器（抓取 + 补全 + 打包 一体化）

打开一个游戏原站（有头浏览器），你手动点击操作（含点进关卡），
工具在后台把「静态资源 + 所有服务端请求/响应（含 WebSocket）」全量抓取落盘，
结束后**自动**：归位资源 → 引擎识别 + 补全缺失 → 打包为**离线可玩**的可导入轻游戏 zip。

## 环境
```
C:\Users\EDY\.workbuddy\binaries\python\envs\pw\Scripts\python.exe
```
（该环境已装 playwright）

## 用法

抓取流水线（一条命令跑完四阶段）：
```
python grab.py <游戏URL> [--out 输出目录] [--id 游戏ID] [--wait 秒]
               [--proxy 代理] [--mobile] [--engine 引擎] [--no-fallback]
               [--no-pack] [--name 游戏名]
```

诊断 / 查询（供人 & AI 调用，输出结构化 JSON）：
```
python grab.py diagnose <抓取目录>              # 可玩性评分 + report.json
python grab.py query <抓取目录> --list-packs    # 引擎识别 + pack 清单
python grab.py query <抓取目录> --list-apis     # 接口端点 + 是否已映射
python grab.py query <抓取目录> --grep "<关键字>" # 在 reqlog / JS / JSON 中检索
python grab.py query <抓取目录> --missing       # 列出缺失资源
python grab.py query <抓取目录> --show <相对路径>  # 查看落盘文件内容摘要
```

示例：
```
python grab.py "http://cm.darknight.games/scriptMaster/web-mobile/index.html" --name 测试1
python grab.py diagnose grab_out/game1_20261011_225819
```

- 浏览器打开后，**手动操作**：进入游戏、逐关点进去、触发需要加载的内容。
- 操作完成后**关闭浏览器窗口**即自动结束（也可在终端按 Ctrl+C），随后自动补全并打包。
- `--wait 30` 可改成自动等 30 秒结束；`--no-pack` 只抓取不打包；`--engine cocos` 强制指定引擎。

## 架构（ggrab 包）
```
grab.py                 入口 CLI（薄壳：pipeline / diagnose / query）
ggrab/
├── capture.py          阶段1 抓取：浏览器驱动 + 全量 hook（含 WS 帧）
├── store.py            落盘布局、reqlog 读写、URL↔路径映射
├── relocate.py         阶段2 归位
├── complete.py         阶段3 补全调度（引擎识别 + 适配器 + 通用回退）
├── engines/            引擎适配器：base(接口) / cocos / generic + 自动识别
├── pack.py             阶段4 打包 + 注入离线补丁
├── shim.py             XHR + fetch + WebSocket 三合一离线补丁
├── http.py             补全用 HTTP GET
├── diagnose.py         可玩性诊断（playable_score）
└── query.py            CLI 查询子命令
```

## 四阶段流水线

| 阶段 | 模块 | 作用 |
|------|------|------|
| 1 抓取 | `capture.py` | 有头浏览器 hook 全量响应；URL 含 `/assets/` 的按镜像落盘，接口响应存 `_api/` |
| 2 归位 | `relocate.py` | 把误存进 `_api/` 的静态资源按原 URL 归位到 `_assets/` |
| 3 补全 | `complete.py` + `engines/` | 自动识别引擎 → 按清单精确补 → 通用回退（reqlog 重放 + JS/JSON 扫描） |
| 4 打包 | `pack.py` + `shim.py` | 生成 `_apidata/`+`_apimap.json`、`_wsmap.json`，注入三合一离线补丁，压出 zip |

## 输出目录布局
```
grab_out/game<ID>_<时间戳>/
├── _assets/<host>/<路径>        静态资源（镜像 URL 路径）
├── _api/<序号>_<host>_<名>.json  接口响应（服务端下发内容）
├── _reqlog.jsonl               逐条请求日志（方法/URL/状态/请求体/落盘路径）
├── _wslog.json                 WebSocket 会话收发帧（打包回放用）
├── summary.json                统计 + 疑似关卡清单 + 拉取模式
├── report.json                 diagnose 产出的诊断报告
├── _build_<名>/                 打包中间产物（含 _apidata/ 与离线补丁的 index.html）
└── <名>.zip                    最终可导入轻游戏的压缩包
```

## 引擎适配器（可插拔）
- 每个适配器实现：`detect()`（识别置信度）/ `find_web_root()` / `collect_manifests()` / `resolve_missing()`。
- 自动识别取最高分；全 0 时回退 `generic`。已内置 **Cocos** 与 **通用回退**；
  Laya/Egret/Unity 可按同样接口增量添加（有样例再补）。

## 抓全与离线的要点
1. **禁缓存**：启动参数 `--disable-http-cache` 保证请求真的发出，且不做任何请求拦截（`page.route` 会卡网络）。
2. **抓 POST body**：在 `request` 阶段拿到（`response` 阶段拿不到）。
3. **响应主线程落盘**：回调只入队，主线程 `drain()` 读 body（Playwright sync API 禁止回调内读 body）。
4. **三路补全**：引擎清单精确补 + 通用回退（reqlog 重放 + JS/JSON 扫描），不依赖人工点全。
5. **三合一离线补丁**：XHR / fetch / WebSocket 全部重定向到本地快照，断网可跑。

## Cocos 落盘规律（补全关键）
- `assets/<pack>/import/<uuid 前2位>/<uuid>.json`：单资源的导入描述。
- `assets/<pack>/import/<uuid 前2位>/<pack名>.json`：打包装聚合文件（pack 名是 9 位短哈希，**不是** uuid）。
- `assets/<pack>/native/<uuid 前2位>/<uuid>.<ext>`：真实二进制，后缀记录在 import json 的 `_native` 字段。

