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

后端可作为一个符合 `uogame-source` 协议的**源**被通用前端接入：

- `GET /.well-known/uogame-source.json` — 源描述（自动发现）
- `GET /api/source/manifest` — 游戏清单（分页 / 增量）
- `GET /api/source/search?q=` — 搜索
- `GET /api/source/game/:id` — 详情

前端支持配置多个源、跨源聚合搜索、在线播放。

## 分支约定

| 分支 | 用途 |
|------|------|
| `main` | 稳定发布分支（经确认的版本） |
| `develop` | 日常开发分支 |

## 许可

个人项目，仅供学习交流。游戏版权归各自原作者所有。
