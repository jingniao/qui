# 构建与测试提速改造规划（方案 A）

> **状态：已被[构建与门禁提速规划](build-speedup-plan.md)（2026-11-12）取代，仅作追溯；规划，尚未实施（2026-11-09 入库）。** 本文描述的做法一条都没落地：`web/package.json` 仍只有 `build`（`vue-tsc --noEmit && vite build`），`web/embed.go` 仍固定读编译期嵌入的 `dist`，测试仍逐例执行全量迁移与生成密钥，`scripts/dev.sh` 也不存在。实施前请按计划逐阶段落地并各自通过门禁；当前行为一律以[项目说明书](../AGENTS.md)与源码为准。

本文档定义在不改变 **qui**（`Go + SQLite + Vue 3`）核心轻量架构、不改变单二进制发布产物、不降低现有门禁标准的前提下，解决“每次修改后构建与测试耗时长”问题的具体执行方案。

---

## 1. 背景与现状瓶颈诊断

当前架构在生产环境具备极低的内存占用（15～30 MB）与零外部运行时依赖优势，但日常开发与验证链路的耗时集中在以下四个环节：

| 瓶颈点 | 现状位置 | 耗时原因 |
| :--- | :--- | :--- |
| **前端类型检查阻塞打包** | `web/package.json` (`build`) | 每次执行 `npm run build` 均串行运行 `vue-tsc --noEmit && vite build`，全量 TypeScript 类型推导显著拖慢纯资源构建。 |
| **前后端静态嵌入强绑定** | `web/embed.go` (`//go:embed all:dist`) | 面板固定从编译期嵌入的 `embed.FS` 读取前端文件，修改前端后必须重新打包前端并重新 `go build` 面板二进制。 |
| **测试库重复执行全量迁移** | `internal/platform/db/db.go`、`internal/platform/server/server_test.go` | `internal/platform/server` 包含 152+ 个重型集成测试，每个用例均在全新临时库上逐条执行 `0001`～`0134` 共 134 个 SQL 迁移文件并计算 SHA-256（累计执行超过 20,000 次迁移语句）。 |
| **测试重复生成非对称密钥** | `internal/platform/server/server_test.go` (`testServer`) | 每个测试实例初始化时均重新生成 ECDSA P-256 根 CA 密钥对、localhost 服务端 TLS 证书以及 `configuration.key` 密封密钥。 |

---

## 2. 改造目标与设计边界

### 2.1 目标指标
* **前端日常修改**：通过 Vite HMR 达到 **< 100ms** 热更新；静态快速构建（`build:fast`）耗时降至 **~2 秒**，且无需重新编译或重启 Go 面板即可刷新生效。
* **后端日常编译**：增量编译 `proxy-panel` / `proxy-agent` 控制在 **1～3 秒**。
* **单包/改动包测试**：通过测试层数据库模板快照与 PKI 模板复用，单包测试耗时降至 **2～5 秒**；全量门禁 `scripts/check.sh` 整体耗时缩短 **40%～60%**。

### 2.2 严格边界（零生产副作用）
1. **零新增依赖**：`go.mod` 与 `web/package.json` 不引入任何第三方库。
2. **零侵入生产数据库逻辑**：SQLite 模板库快照与 PKI 身份缓存**仅在测试辅助层生效**，生产环境 `db.Open`、`db.Migrate` 与 `pki.LoadOrCreate` 代码逻辑保持原样。
3. **发布与门禁 100% 兼容**：`scripts/build-release.sh` 与 `scripts/check.sh` 的行为、产物格式、迁移不可变校验（`migrations/checksums_test.go`）完全保持不变。

---

## 3. 分阶段实施步骤

### 阶段 1：前端构建与类型检查解耦

#### 步骤 1.1：拆分 `web/package.json` 构建命令
* **目标文件**：`web/package.json`
* **修改内容**：新增 `build:fast` 脚本，仅运行 `vite build`；保留原 `build` 供 CI 与发版脚本调用。
```json
  "scripts": {
    "dev": "vite",
    "typecheck": "vue-tsc --noEmit",
    "build": "vue-tsc --noEmit && vite build",
    "build:fast": "vite build",
    "preview": "vite preview"
  }
```

#### 步骤 1.2：同步根目录 `package.json`
* **目标文件**：`package.json`
* **修改内容**：新增 `"build:fast": "npm --prefix web run build:fast"`。

#### 步骤 1.3：增强 `web/vite.config.ts` 本地代理灵活性
* **目标文件**：`web/vite.config.ts`
* **修改内容**：支持通过环境变量 `VITE_PANEL_TARGET` 指定后端面板地址（默认 `http://127.0.0.1:8081`），并在 `proxy` 中启用 `secure: false` 以兼容面板自签名 HTTPS 证书。
```ts
const panelTarget = process.env.VITE_PANEL_TARGET || 'http://127.0.0.1:8081'
// server.proxy 配置：
proxy: {
  '/api': { target: panelTarget, changeOrigin: false, secure: false },
  '/healthz': { target: panelTarget, changeOrigin: false, secure: false },
  '/sub': { target: panelTarget, changeOrigin: false, secure: false }
}
```

---

### 阶段 2：支持外部静态资源目录挂载（免重编 Go）

#### 步骤 2.1：扩展 `web/embed.go`
* **目标文件**：`web/embed.go`
* **修改内容**：在 `Files()` 中支持 `QUI_WEB_DIR` 环境变量覆盖。未设置该变量时（默认生产环境与单元测试），完全沿用编译期嵌入的 `embed.FS`。
```go
package web

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed all:dist
var assets embed.FS

func Files() fs.FS {
	if override := os.Getenv("QUI_WEB_DIR"); override != "" {
		return os.DirFS(override)
	}
	files, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	return files
}
```
* **使用方式**：
  * 启动面板时设置 `QUI_WEB_DIR=/path/to/qui/web/dist`。
  * 修改前端代码后运行 `npm run build:fast`（约 1.5～2 秒），直接刷新浏览器即可加载最新前端资源，无需重编 Go 二进制，也不会中断已连接的 Agent gRPC 会话。

---

### 阶段 3：测试底座快照化（SQLite 模板库 + PKI 模板复用）

> **安全原则**：本阶段仅修改 `*_test.go` 或测试专用辅助函数，不修改生产环境 `db.Open` 的迁移执行路径。

#### 步骤 3.1：在 `internal/platform/server/server_test.go` 引入基准快照
* **目标文件**：`internal/platform/server/server_test.go`
* **实现逻辑**：
  1. **数据库模板快照 (`templateDBOnce`)**：
     * 在当前测试进程首次调用时，于临时目录创建并执行一次完整的 `db.Open(context.Background(), tempPath)`（完成 `0001`～`0134` 迁移）。
     * 执行 `PRAGMA wal_checkpoint(TRUNCATE)` 与 `PRAGMA journal_mode = DELETE`（或关闭连接确保所有页落盘到主文件），将生成的干净 SQLite 文件读入内存 `templateDBBytes []byte`。
     * 每个调用 `testServer(t)`（及相关辅助函数）的测试，先将 `templateDBBytes` 直接写入 `filepath.Join(t.TempDir(), "panel.db")`（权限 `0600`），再调用 `db.Open`。此时 `db.Open` 检测到表与 `schema_migrations` 均已就绪，跳过 134 次 `CREATE/ALTER/INSERT` 执行。
  2. **PKI 与 Vault 身份模板 (`templateIdentityOnce`)**：
     * 在测试进程首次调用时，生成一份标准的测试用 `ca.crt`、`ca.key`、`server.crt`、`server.key` 与 `configuration.key` 缓存在内存中。
     * `testServer(t)` 初始化 `c.NodeIdentityDirectory` 时直接写出这 5 个文件，使 `server.New` 直接加载现有密钥材料，免去每个测试用例重复执行 2 次 ECDSA P-256 密钥生成与证书签发。

#### 步骤 3.2：在 `internal/platform/db/db.go` 缓存内置迁移文件读取与校验和
* **目标文件**：`internal/platform/db/db.go`
* **修改内容**：
  * 当 `Migrate(ctx, database, files)` 传入的 `files` 为内置 `migrations.Files` 时，使用进程级 `sync.Once` 缓存排序后的文件名列表、SQL 文本及对应的 SHA-256 校验和字符串。
  * 这属于纯内存只读缓存，不改变任何 SQL 执行与事务校验语义，但消除了同一进程内数百次重复的 `fs.ReadFile` 与 `sha256.Sum256` 计算开销。

---

### 阶段 4：新增日常极速开发脚本 `scripts/dev.sh`

#### 步骤 4.1：创建 `scripts/dev.sh`
* **目标文件**：`scripts/dev.sh`
* **提供子命令**：
  1. `bash scripts/dev.sh web`：执行 `npm --prefix web run build:fast`（2 秒极速打包前端）。
  2. `bash scripts/dev.sh build`：增量编译宿主架构 `bin/proxy-panel` 与 `bin/proxy-agent`（不跑 `npm ci`、不交叉编译双架构、开启并行编译）。
  3. `bash scripts/dev.sh test [pkg...]`：
     * 若未指定包名，自动通过 `git status --porcelain` 检测工作区修改过的 Go 包并运行 `go test`（默认不带 `-race`）。
     * 若指定包名（如 `bash scripts/dev.sh test ./internal/panel/proxy`），仅针对目标包运行秒级测试。

---

## 4. 验证与回归检查清单

实施完成后，按顺序执行以下命令验证改动的正确性与提速效果：

1. **前端快速构建与类型检查验证**
   ```bash
   npm run build:fast
   npm run typecheck
   ```
   * 预期：`build:fast` 在数秒内完成并生成 `web/dist/index.html`；`typecheck` 零报错退出。

2. **数据库迁移与不可变校验和回归验证**
   ```bash
   go test -v ./migrations ./internal/platform/db ./internal/platform/pki
   ```
   * 预期：`checksums_test.go`、`TestOpenMigratesAndPersists`、`TestFailedMigrationRollsBackAndCanRetry` 全部通过。

3. **Server 集成测试提速验证**
   ```bash
   go test ./internal/platform/server -run 'TestHTTP|TestNode'
   ```
   * 预期：全部通过，且单测初始化耗时显著降低。

4. **全量门禁兼容性验证（Linux 测试机 / CI）**
   ```bash
   bash scripts/check.sh
   ```
   * 预期：全流程退出码为 `0`，发布构建脚本 `scripts/build-release.sh` 产物与改造前完全一致。
