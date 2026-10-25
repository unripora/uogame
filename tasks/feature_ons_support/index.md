# 任务：新增 ONS（NScripter）游戏类型

> **类型**：feature
> **优先级**：P1
> **负责人**：AreaSongWcc
> **状态**：🟡 进行中（核心链路已实测通过，待验证存读档）
> **开始时间**：2026-10-25
> **预计完成**：待定（依赖用户提供测试游戏包）

## 🎯 目标

让网站支持在浏览器本地运行 ONS（NScripter）文字冒险游戏：管理员导入游戏包 → 前台点开即玩，不依赖串流或服务器转码。

技术底座：[OnscripterYuri](https://github.com/YuriSizuku/OnscripterYuri) 的 WebAssembly 构建（GPL-3.0），存档写 IndexedDB，支持导出/导入存档 zip。

## 📌 范围

### 做
- 新增 `game_type = ONS`，前端/后端类型体系打通
- 引擎静态资源内置到 `web/public/ons/`
- 新增 ONS 播放器页，按游戏目录动态生成 OnscripterYuri 所需的 `onsyuri_index.json`
- 管理员通过「游戏包 zip + config.json」导入 ONS 游戏
- 中文 GBK / 日文 SJIS / UTF-8 脚本编码可选
- 存档：使用引擎自带 IndexedDB 存档 + 存档 zip 导出/导入

### 不做（本期）
- 云端存档与 `/api/user/backups` 打通（备份接口是 JSON 文本，ONS 存档是二进制 zip，二期再设计 base64/对象存储）
- 视频播放（浏览器端解码能力有限，默认 `--no-video`）
- 依赖自定义 DLL / 注册表的 ONS 变体
- YU-RIS 等其他闭源引擎

## 🔍 现状调研

| 模块 | 位置 | 结论 |
|---|---|---|
| 游戏模型 | `server/internal/models/models.go:106-136` | 已有 `GameType`（默认 H5）、`MountPath`、`SizeBytes` |
| 导入配置 | `server/internal/api/handlers/game.go:27-41` | `GameConfig` 暂缺 `game_type` / `ons_args` |
| 游戏包导入 | `server/internal/api/handlers/game.go:986-1155` | 要求 zip 根目录有 `config.json`，解包到 `GamesDir`，写 `MountPath` |
| 源文件导入 | `server/internal/api/handlers/game.go:1220-1390` | 单文件上传，可按表单 `game_type` 建库（不适合 ONS 多文件目录） |
| 游戏文件服务 | `server/internal/api/handlers/game.go:837-894` | `/play/:id/*file`，支持 Range、MIME 白名单、路径穿越防护 |
| 文件列表 | `server/internal/api/handlers/game.go:669-698` | 只扫描 ROM 扩展名，ONS 文件不在其中，需新增清单接口 |
| 静态资源 | `server/internal/api/router.go:202-229` | `NoRoute` 优先返回 `uiDir` 真实文件，`web/public/*` 构建后可直接访问 |
| 前端类型 | `web/src/lib/gameType.js:4-35,132-138` | 需新增 `ONS` 与 `playKind='ons'` |
| 播放派发 | `web/src/views/PlayView.jsx:142-192` | H5/Flash/ROM 三分支，新增 ONS 分支 |
| 播放器范例 | `web/public/emulator/emulatorjs.html:536-545,1513-1518` | 已有 `gameId + token → /play/...` 的成熟契约，可复用 |
| 管理端 | `web/src/views/admin/GameManagePanel.jsx:384-386` | 类型下拉来自 `GAME_TYPES`，加类型后自动生效 |

## 🧪 测试资源验证（120日元 星翼压缩版）

路径：`F:\小智的仓库\120日元 星翼压缩版`，共 6 个文件、335.9MB。

| 文件 | 大小 | 结论 |
|---|---|---|
| `nscript.dat` | 763KB | 标准 NScripter 编译脚本（非文本），ONScripter 可直接读取 |
| `arc.nsa` | 113.7MB | 标准旧版 NSA 归档，1017 个文件 |
| `arc1.nsa` | 79.7MB | 标准旧版 NSA 归档，2064 个文件（语音 ogg） |
| `arc2.nsa` | 152.8MB | 标准旧版 NSA 归档，103 个文件（bgm mp3） |
| `default.ttf` | 2.8MB | 自带字体，需一并打包 |
| `Windows PC版（带图标）.zip` | 2.3MB | 仅含 Windows EXE，**不放入网页包** |

归档结构已逐一校验（大端字段）：

```
uint16 文件数
uint32 base_offset
[文件名\0][compression_type 1B][offset 4B][length 4B][original_length 4B] ...
```

- 三个归档的 `offset + length + base_offset` 均精确等于文件大小，条目边界连续
- `compression_type` 为 0/1/2（无压缩 / SPB / NBZ），均为 ONScripter 内置解压类型，无需自行解包
- 结论：**这是标准 ONS 包，可直接交给 OnscripterYuri 运行，不需要转换或重打包**
- 待实测确认：文字编码（标题与字体提示为汉化版，预期 `--enc:gbk`）

网页包只取：`nscript.dat`、`arc.nsa`、`arc1.nsa`、`arc2.nsa`、`default.ttf`（约 349MB）。

## 🏗️ 方案

### 数据流

```
管理员导入 zip(config.json + ONS 文件)
        │
        ▼
GamesDir/<游戏名>/  ── MountPath ──┐
                                   │
前台 /play/:id  ── iframe ──> /ons/player.html?gameId=&token=
                                   │
                                   ├─ GET /api/games/:id/ons-manifest?token=
                                   │      （扫描目录 → 文件清单 + args）
                                   ├─ 生成 Blob manifest（URL 带 token）
                                   ├─ <meta onsyuri_index=BlobURL>
                                   └─ 加载 /ons/onsyuri.js + .wasm
                                          │
                                          └─ fetch /play/:id/<file>?token=
存档 ── Emscripten IDBFS ──> 浏览器 IndexedDB（savedir=/onsyuri_save/game<id>）
```

### 引擎放置

```
web/public/ons/
├── onsyuri.html          # 引擎自带页面（备用）
├── onsyuri.js
├── onsyuri.wasm
├── player.html           # 我们新增的适配页（注入动态 manifest）
└── LICENSE               # GPL-3.0 说明
```

构建产物来源：GitHub Releases 的 web 包，或 `build_web` Actions artifact。

### 后端接口

`GET /api/games/:id/ons-manifest`（需登录，支持 `?token=`）

响应：

```json
{
  "title": "游戏名",
  "args": ["--enc:gbk", "--no-video"],
  "savedir": "/onsyuri_save/game12",
  "files": ["nscript.dat", "0.txt", "arc.nsa", "bgm/01.ogg"]
}
```

- 递归扫描 `MountPath`（限制深度、跳过隐藏文件与 `config.json`）
- `args` 优先取 `config.json` 的 `ons_args`，缺省 `["--enc:gbk", "--no-video"]`
- 不返回 token，由播放器自行拼接 `/play/:id/<path>?token=`

### config.json 扩展

```json
{
  "name": "游戏名",
  "game_type": "ONS",
  "ons_args": ["--enc:gbk"],
  "cover_path": "cover.jpg",
  "tags": ["ONS"]
}
```

- `GameConfig` 增加 `GameType`、`OnsArgs`
- `buildGameFromConfig` 写入 `GameType`（空则 H5）
- 旧包不含新字段时行为不变

### 前端改动

- `gameType.js`：新增 `{ value: 'ONS', label: 'ONS (NScripter 视觉小说)', kind: 'ons' }`；`playKind` 返回 `ons`
- `PlayView.jsx`：`kind === 'ons'` 时 iframe 指向 `/ons/player.html?gameId=...&token=...`
- 详情页：ONS 不显示「模拟器内核」提示，改为「浏览器本地运行（ONS）」

### 播放器页 player.html 逻辑

1. 解析 `gameId`、`token`（兼容历史 `id` 参数）
2. `fetch('/api/games/{id}/ons-manifest?token=...')`
3. 把 `files` 映射为 `{ path, url: '/play/{id}/{encodeURI(path)}?token=...' }`
4. 组装完整 manifest，`URL.createObjectURL(new Blob([json]))`
5. 设置 `<meta onsyuri_index="blob:...">`，再动态插入 `/ons/onsyuri.js`
6. 失败时展示中文错误页（游戏不存在 / 无文件 / 引擎加载失败）

## 📋 工作分解（WBS）

| ID | 任务 | 状态 | 优先级 | 说明 |
|----|------|------|--------|------|
| 0.1 | 获取 OnscripterYuri web 构建产物 | ✅ | P0 | v0.7.7beta（onsyuri.html/js/wasm）|
| 0.2 | 准备测试 ONS 游戏包 | ✅ | P0 | 已确认标准 NSA 结构，无需转换 |
| 1.1 | 引擎资源入库 `web/public/ons/` | ✅ | P0 | 含 GPL-3.0 LICENSE |
| 1.2 | 编写播放器适配页 | ✅ | P0 | 改为让 `onsyuri.html` 支持 `?index=`（无需独立 player.html）|
| 2.1 | 新增 `ons-manifest` 接口 | ✅ | P0 | 扫描目录 + args + lazyload（>64MB 启用）|
| 2.2 | 补充游戏文件 MIME 白名单 | ✅ | P1 | `.dat`/`.nsa`/`.ns2`/`.nbz`/`.spb` |
| 2.3 | `config.json` 支持 `game_type`/`ons_args` | ✅ | P1 | 兼容旧包（字段缺省行为不变）|
| 2.4 | `/ons/*` 静态头处理（COOP/COEP，如需） | ✅ | P2 | 引擎为单线程构建，无需 SharedArrayBuffer |
| 3.1 | 前端类型与 `playKind` | ✅ | P0 | gameType.js |
| 3.2 | PlayView 新增 ONS 分支 | ✅ | P0 | iframe 指向 `/ons/onsyuri.html?index=...` |
| 3.3 | 详情页文案适配 | ✅ | P2 | 显示「浏览器本地运行」 |
| 4.1 | 中文 GBK 游戏全流程实测 | ✅ | P0 | 标题画面/立绘/文字正常，lazyload 生效 |
| 4.2 | 日文 SJIS 实测（有资源时） | 🔵 | P2 | `--enc:sjis` |
| 4.3 | 存档 zip 导出/导入验证 | 🔵 | P1 | 引擎自带功能，待人工验证 |
| 5.1 | README 补充 ONS 打包规范 | 🔵 | P2 | 目录与 config 示例 |

## ⚠️ 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 引擎需要 SharedArrayBuffer | 加载失败 | 对 `/ons/*` 加 COOP/COEP；或改用无线程构建 |
| 游戏依赖自定义 DLL / 注册表 | 启动失败 | 界面明确提示"该 ONS 变体不支持"，不强行兼容 |
| 视频解码缺失 | 过场动画黑屏 | 默认 `--no-video`，在文档中说明 |
| 汉化脚本编码不同 | 乱码 | `ons_args` 支持 `--enc:gbk` / `--enc:sjis` / `--enc:utf8` |
| 首次加载慢（大包） | 体验差 | manifest 启用 `lazyload`，逐文件按需拉取 |
| 存档只在单浏览器 | 换设备丢失 | 提供存档 zip 导出/导入；二期接备份 API |
| GPL-3.0 许可 | 分发合规 | 保留 LICENSE，站点仅自用不分发引擎源码修改版 |

## ✅ 验收标准（DoD）

- [x] 管理员导入 ONS zip 后，前台详情页出现「开始游戏」
- [x] 进入游戏后文字、背景正常，中文不乱码（BGM/语音未单独验证，留待 4.3 一并实测）
- [ ] 存读档可用，刷新页面后存档仍在（IndexedDB）
- [ ] 存档可导出为 zip 并再次导入恢复
- [x] 未携带 token 无法访问 `/play/:id/*` 游戏文件（无 token / 错误 token 均 404）
- [ ] H5 / ROM / Flash 现有玩法回归正常（本次仅新增分支，未做实机回归）
- [x] `cd web && npm run build` 通过
- [x] `cd server && go test ./...`：本次改动无新增失败；仓库基线存在 3 个无关用例失败（`TestAccessiblePaths` / `TestBrowseAccessiblePath` / `TestInjectSaveNamespace`，已用 stash 验证为改动前既有）

## ❓ 待确认

1. 测试游戏的具体引擎变体（纯 ONS / ONScripter 增强 / 带 DLL）
2. 游戏包目录结构（是否含 `nscript.dat`、`0.txt`、`*.nsa`、`default.ttf`）
3. 是否需要支持日文原版（决定默认编码与字体策略）

## 📈 变更日志

| 时间 | 操作 | 说明 |
|------|------|------|
| 10-25 | ✅ 手动填写 config.json | 导入面板每个 zip 支持展开填写 config.json（含填入模板），以 `config_json` 表单提交；后端优先使用手填内容，zip 可不再包含 config.json，缺 name/game_type 时回退界面输入 |
| 10-25 | ✅ 悬浮菜单适配小屏 | 面板改为 2 列网格并限制高度（`max-height: calc(100vh - 88px)`）+ 可纵向滚动，解决横屏/全屏时顶部「菜单」按钮超出屏幕点不到的问题 |
| 10-25 | ✅ 画质档位 | 悬浮菜单新增「画质」：平滑 / 锐利（`image-rendering: pixelated`，即时切换不重载）。曾尝试引擎 `--width/--height` 强制高分辨率，但该 web 构建下会导致触摸坐标与 640×480 逻辑分辨率错位、点击失效，已回退 |
| 10-25 | ✅ 修复悬浮菜单 | 移除右键/长按/F9 弹出的英文快捷菜单（sfull/full/menu 等）；修复「菜单」「跳过」无效：合成 KeyboardEvent 时 keyCode/which 需手动 defineProperty 覆盖（构造参数中的 keyCode 会被浏览器忽略） |
| 10-25 | ✅ 移动端悬浮菜单 | 游戏内新增可收起的悬浮按钮（☰）：菜单/跳过/全屏/拉伸/导出存档/导入存档，避免依赖长按调出引擎菜单；点击按键不会误触游戏 |
| 10-25 | ✅ 移动端优化 | 移除阻塞解析的 CDN JSZip（改为存档导出/导入时按需加载）；新增加载进度条（文件名 + 字节/百分比）与错误提示；修正 `onsinit` 提前收起进度条的问题（改为等待在途下载结束） |
| 10-25 | ✅ 实机验证 | 导入「120日元」后标题画面正常渲染（640×480），lazyload 生效，token 鉴权 404 生效 |
| 10-25 | 🟡 待验证 | 存读档持久化、存档 zip 导入导出、音频与旧玩法回归 |
| 10-25 | ✅ 前端接入 | `GAME_TYPES` 增 ONS；PlayView 增 ONS 分支；详情页文案适配 |
| 10-25 | ✅ 修复缺失接口 | 补注册 `GET /api/games/:id`（前端详情深链所需，仓库基线缺失） |
| 10-25 | ✅ 后端接口 | 新增 `GET /api/games/:id/ons-manifest`；`config.json` 支持 `game_type`/`ons_args`；补 ONS MIME |
| 10-25 | ✅ 播放器适配 | `onsyuri.html` 支持 `?index=` 动态 manifest（回退 meta） |
| 10-25 | ✅ 引擎入库 | `web/public/ons/` 放入 onsyuri.html/js/wasm + GPL-3.0 LICENSE |
| 10-25 | ✅ 验证测试资源 | 确认 `120日元 星翼压缩版` 为标准 NSA，可直接运行 |
| 10-25 | 📝 创建计划 | 完成现状调研与 WBS |
