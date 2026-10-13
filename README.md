# 拾游集 UOGame

个人自托管的**网页游戏库**：把 H5 / ROM / Flash 小游戏集中收录，支持浏览、搜索、在线游玩、评分、存档、多人联机房间，并可作为一个「**游戏源**」被通用前端在线拉取。

> 个人项目，非商业用途。游戏本体与运行时数据不入库。

## 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go 1.22 + Gin + GORM + SQLite |
| 前端 | React 18 + Vite + Tailwind CSS |
| 播放内核 | EmulatorJS（ROM）/ Ruffle（Flash） |
| 认证 | JWT（HttpOnly Cookie / Bearer / `?token=` 三通道） |

## 目录结构

```
UOGame/
├── server/                 # Go 后端
│   ├── cmd/server/         # 入口
│   ├── internal/
│   │   ├── api/            # 路由 + handlers + middleware
│   │   ├── config/         # 配置（环境变量）
│   │   ├── database/       # 数据库初始化 / 迁移
│   │   └── models/         # 数据模型
│   └── ui/                 # 前端构建产物（运行目录）
├── web/                    # 前端源码（React + Vite）
│   ├── src/
│   └── public/emulator/    # 模拟器内核（EmulatorJS / Ruffle）
└── build.ps1               # 一键构建（前端 + 后端交叉编译 + 打包）
```

## 运行方式

### 后端

```bash
cd server
go mod download
# 关键环境变量（均有默认值，生产务必覆盖）
#   PORT            监听端口，默认 8666
#   DATA_DIR        数据目录（数据库 / 存档）
#   JWT_SECRET      JWT 密钥（缺省自动生成并落盘）
#   ADMIN_USERNAME / ADMIN_PASSWORD   初始管理员
#   CORS_ORIGINS    允许跨域的来源（逗号分隔，供「游戏源」模式）
go run ./cmd/server
```

### 前端

```bash
cd web
npm install
npm run dev      # 开发（代理 /api、/play 到 :8666）
npm run build    # 构建到 web/dist（供后端 ui 目录使用）
```

### 一键构建

```powershell
powershell -ExecutionPolicy Bypass -File .\build.ps1
```

## 游戏源（Source）模式

本项目内建一套**游戏源协议**（`uogame-source`），把「游戏元数据 + 游戏文件」的提供方抽象为可插拔的**源**。用户在前端配置源地址即可在线浏览与游玩，无需把游戏库部署在同一处。

支持两类源，前端用**同一套源管理**统一处理：

| 源类型 | `type` | 清单来自 | 播放来自 | 计入排行榜 |
|--------|--------|----------|----------|-----------|
| **动态源** | `dynamic` | 后端 API | 后端 `/play/:id/*file` | ✅ 仅本地源 |
| **静态源** | `static` | GitHub raw 的 `manifest.json` | GitHub raw 文件直链 | ❌ 不计入 |

### 动态源（本服务自身即一个源）

后端按协议对外暴露以下接口，可被任意符合规范的前端接入：

- `GET /.well-known/uogame-source.json` — 源描述（自动发现）
- `GET /api/source/manifest` — 游戏清单（分页 / 增量）
- `GET /api/source/search?q=` — 搜索
- `GET /api/source/game/:id` — 详情
- `GET /play/:id/*file` — 游戏文件（在线播放，支持 Range）

**源描述示例：**

```json
{
  "protocol": "uogame-source",
  "version": 1,
  "type": "dynamic",
  "name": "拾游集",
  "baseUrl": "https://your-host",
  "capabilities": { "browse": true, "search": true, "detail": true, "stream": true, "offline": false, "local": true },
  "endpoints": {
    "manifest": "/api/source/manifest",
    "search": "/api/source/search",
    "detail": "/api/source/game/{id}",
    "stream": "/play/{id}/{file}"
  },
  "auth": { "type": "bearer" }
}
```

### 静态源（GitHub 仓库当源，Mihon 风格）

游戏文件可完全外置到 **GitHub 仓库**，无需自建后端。

> ⚠️ **必须开启 GitHub Pages**：GitHub raw 与 jsDelivr 对 HTML 返回 `Content-Type: text/plain`，浏览器会把游戏页面当纯文本显示、无法运行。**只有 GitHub Pages 返回 `text/html` 能正常渲染**（实测结论见任务文档）。

**仓库结构约定：**

```
your-repo/（开启 GitHub Pages）
├── uogame-source.json     # 源描述
├── manifest.json          # 精简游戏列表（列表页字段）
├── details/               # 游戏详情分片（可选，点进去才拉）
│   └── {id}.json
└── games/                 # 游戏文件本体
    └── {id}/
        ├── index.html     # 入口
        ├── cover.png
        └── ...
```

**源描述（`uogame-source.json`）：**

```json
{
  "protocol": "uogame-source",
  "version": 1,
  "type": "static",
  "name": "某人的游戏仓库",
  "baseUrl": "https://<user>.github.io/<repo>",
  "manifest": "manifest.json"
}
```

> `baseUrl` 必须是 **GitHub Pages 域名**（`https://<user>.github.io/<repo>`），不能用 raw.githubusercontent.com。

**清单（`manifest.json`，只放列表页需要的字段以控制体积）：**

```json
{
  "version": 1,
  "updated_at": 1791614509,
  "count": 2642,
  "games": [
    {
      "id": "island-hd",
      "name": "荒岛求生HD",
      "game_type": "H5",
      "cover": "games/island-hd/cover.png",
      "path": "games/island-hd/",
      "entry": "index.html"
    }
  ]
}
```

- 播放地址 = `baseUrl + "/" + path + entry`
- 游戏详情可另放 `details/{id}.json`（简介 / 标签等），**缺失不影响列表与播放**

**前端能力：** 配置多个源、跨源聚合搜索、在线播放；静态源清单在本地做关键词过滤。

> ⚠️ 合规与限制：公开 GitHub 仓库存放含版权游戏存在 **DMCA 删库风险**；GitHub raw 并非 CDN，大流量播放会被限流，单文件上限 100MB。请自行评估使用场景。

## 分支约定

| 分支 | 用途 |
|------|------|
| `main` | 稳定发布分支（经确认的版本） |
| `develop` | 日常开发分支 |

## 许可

个人项目，仅供学习交流。游戏版权归各自原作者所有。
