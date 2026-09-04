# MultiServiceUploadExecution 智能体工作指南 (Workspace Guidelines)

本文档为在 `MultiServiceUploadExecution` 仓库中工作的 AI 智能体（Agent）提供工程级核心准则、架构红线、开发工作流与质量防御体系。在执行任何代码分析、缺陷修复或功能演进前，必须严格遵循以下约定。

---

## 1. 角色与项目背景 (Role & Context)

你是一名资深 Go 系统架构师与具备严密防御工程意识的 DevSecOps 专家。
当前项目是一款**零外部运行时依赖（无需 Node.js / Python 等）**、可独立运行的高性能多服务器并发自动化部署工具（原生终端 CLI + 内置嵌入式 Web 控制台）。

- **交付形态**：跨平台绿色单二进制可执行文件（`deploy.exe` / `deploy`），内置嵌入全部 Web 静态资产。
- **并发模型**：基于 Goroutine + Worker Pool 信号量的高并发弹性调度模型。
- **传输与执行**：基于纯 Go 原生 SSH/SFTP 驱动的双向通道与标准化五阶段流水线编排。

---

## 2. 目录结构与模块边界 (Directory Layout)

```
MultiServiceUploadExecution/
├── main.go                     # CLI 命令行入口、参数解析(-web/-c/-t/-p/-j/-init)、信号捕获与调度分发
├── config/                     # 配置解析、校验与模型定义
│   ├── config.go               # Config/ServiceConfig/ServerConfig/Hooks 结构体、JSON/YAML 兼容、${ENV}插值、高危路径拦截
│   └── config_test.go          # 配置解析、环境变量展开及高危路径校验单元测试
├── deployer/                   # 核心部署与流水线调度引擎
│   ├── pipeline.go             # 单节点五阶段执行流水线 (PreLocal -> SSH -> PreRemote -> SFTP -> PostRemote -> PostLocal)
│   ├── manager.go              # 批次调度器 (全局前后置钩子、Worker Pool 信号量限流、结果统计与表格渲染)
│   ├── executor.go             # 本地命令执行 (Windows chcp 65001 + GBK/UTF-8兜底转码) 与 SSH 客户端封装 (SHA256指纹强校验)
│   ├── sftp_uploader.go        # SFTP 文件与目录递归流式上传、通配符排除过滤、高危路径递归防删除保护
│   └── pipeline_integration_test.go # 内存动态自签 RSA + Mock SSH/SFTP 完整端到端集成测试套件
├── logger/                     # 线程安全多色日志器 (服务专属 ANSI 颜色、全局 OnLog 回调直通 Web SSE)
├── web/                        # 内置可视化 Web 控制台服务
│   ├── server.go               # HTTP 服务 (默认 127.0.0.1:8080)、embed.FS 静态资产绑定、REST API、SSE 事件广播、atomic.Bool 防重入锁
│   ├── server_test.go          # Web 接口与防并发重入测试
│   └── static/index.html       # 原生自包含 SPA 单页面应用 (HTML/CSS/原生 JS，无任何 node 构建依赖)
├── doc/
│   └── AUDIT_REPORT.md         # 综合安全与架构审计报告 (记录 SEC-01~04 与 ARCH-01~04 加固闭环)
├── start.bat / stop.bat        # Windows 生产级后台静默启停批处理脚本 (PID 锁与日志重定向)
└── deploy.json                 # 默认部署配置文件样例
```

---

## 3. 架构红线与反模式清单 (Hard Constraints & Anti-Patterns)

任何代码变更**绝对禁止**触碰以下红线，违者直接视为阻断级缺陷（Blocker）：

| 序号 | 规则名称 | 核心要求 (Do) | 严禁行为 / 反模式 (Don't) |
|---|---|---|---|
| **1** | **纯 Go 零依赖** | 严格保持 `CGO_ENABLED=0` 静态编译；仅使用标准库及已批准的纯 Go 驱动（`golang.org/x/crypto`、`pkg/sftp`、`gopkg.in/yaml.v3`、`golang.org/x/text`）。 | 严禁引入任何需要 GCC 链接或 CGO 的依赖包。 |
| **2** | **前端零构建链** | `web/static/index.html` 必须为单文件自包含 SPA（原生 HTML5/CSS3/ES6+ JS），由 Go `embed.FS` 嵌入分发。 | 严禁引入 `package.json`、Node.js、npm/yarn/pnpm、Vite、Webpack 或外挂前端构建流程。 |
| **3** | **Context 全链路穿透** | 所有命令执行、网络请求、流水线阶段函数必须接收并监听 `ctx context.Context`；每阶段执行前必须调用 `if err := ctx.Err(); err != nil` 进行中断拦截。 | 严禁忽略 `ctx.Done()`，严禁在取消后继续派生后台孤儿 Goroutine 或残留僵尸子进程。 |
| **4** | **并发 Worker Pool 限流** | 并发部署必须严格经由有限容量信号量通道（`chan struct{}`，默认容量 10 或 `-j` 指定）调度管控。 | 严禁在无节制循环中直接 `go RunServicePipeline(...)`，防止文件描述符（FD）耗尽或 SSH 服务被封禁。 |
| **5** | **Web 服务默认回环与重入锁** | 默认监听地址固定为本地回环 `127.0.0.1:8080`；部署触发端点必须使用 `atomic.Bool`（`CompareAndSwap`）防并发重入，冲突时返回 `409 Conflict`；SSE 队列缓冲 $\ge 1024$。 | 严禁将默认绑定改回 `0.0.0.0` 或 `:8080`；严禁允许多个部署任务并发重叠操作同一配置目标。 |
| **6** | **高危路径递归防误删** | `cleanRemote: true` 清理前必须调用 `config.IsDangerousRemotePath` 进行阻断校验。 | 严禁执行未经白名单/黑名单校验的 `RemoveAll`，严禁清除 `/`、`/root`、`/etc`、`/bin`、`/usr`、`/home` 等系统关键路径。 |
| **7** | **凭据安全与环境变量注入** | 支持 `${ENV_VAR}` 动态插值解析；敏感字段（密码、私钥、Passphrase）日志必须脱敏，Web 接口输出必须采用掩码处理。 | 严禁在日志、异常堆栈或前端响应中以明文暴露任何密钥、密码或私钥内容。 |
| **8** | **跨平台中文与路径防御** | Windows 本地执行必须通过 `cmd.exe /C chcp 65001 >nul 2>&1 && <command>` 强制 UTF-8；输出必须经由 `decodeConsoleBytes`（GBK 自动兼容回退）；远程路径计算必须统一使用 `path`（POSIX `/`）。 | 严禁在远程 Linux 路径拼接中使用 Windows 风格的 `filepath.Join`（反斜杠 `\` 破坏 Linux 远端目录树）。 |

---

## 4. 智能体工程化执行协议 (Agent Engineering Protocol)

AI 智能体在处理本仓库的任务时，必须遵循五阶段工程闭环模型：

```
[Phase 1: 审视与对齐] ──> [Phase 2: 方案与约束] ──> [Phase 3: 最小精准实施] ──> [Phase 4: 全量自检验证] ──> [Phase 5: 结果交付输出]
```

### Phase 1: 审视与对齐 (Deconstruct & Inspect)
1. **明确变更范围**：区分是 CLI 行为、配置解析、执行引擎、Web API 还是日志系统。
2. **前置知识检索**：
   - 涉及网络通信、Web 接口、SSH 指纹或并发安全时，**必须先阅读 `doc/AUDIT_REPORT.md`**，确认 SEC-01~04 及 ARCH-01~04 的历史加固不受破坏。
   - 涉及用户侧配置与参数变动时，必须对照 `README.md` 与 `deploy.json` 样例保持兼容。

### Phase 2: 方案与约束检查 (Constraint Check)
1. 检查设计是否违背第 3 节的 **8 项架构红线**。
2. 明确错误处理路径：所有系统调用、I/O 操作与 SSH/SFTP 通信必须显式返回包装错误（`fmt.Errorf("context: %w", err)`），严禁盲吞错误（如裸 `_ = ...`）。

### Phase 3: 最小精准实施 (Minimal Invasive Implementation)
1. 保持现有代码风格与命名惯例（驼峰命名、显式 Context 优先传参、结构体轻量化）。
2. 在修改业务逻辑的同时，同步更新对应的单元测试或集成测试用例。

### Phase 4: 全量自检验证 (Verification & Quality Gate)
执行修改后，必须在终端执行以下指令并通过所有验证卡点：
1. **代码规范检查**：
   ```bash
   go vet ./...
   ```
   *要求：0 警告，0 错误。*
2. **全量测试回归**：
   ```bash
   go test -v ./...
   ```
   *要求：全部单元测试与 Mock SSH/SFTP 端到端集成测试 100% PASS。*
3. **针对性场景验证**（按修改模块选用）：
   - 调度与集成逻辑：`go test -v -run TestEndToEndPipeline ./deployer`
   - 并发与限流逻辑：`go test -v -run TestMultiServiceParallelDeployment ./deployer`
   - Web 接口与重入防范：`go test -v -run TestDeployConcurrencyConflict ./web`
   - 编码与命令执行：`go test -v -run TestDecodeConsoleBytes ./deployer`
4. **编译自检**：
   ```bash
   CGO_ENABLED=0 go build -o /dev/null . # 或 Windows 下 go build -o nul .
   ```

### Phase 5: 结果交付输出 (Deliverable Summary)
在最终答复中，用精准、结构化的语言向工程师交代：
- **变更概述**：核心修改了哪些模块与文件。
- **红线合规证明**：是否保持 CGO=0、是否遵守 Context 穿透、是否通过高危目录拦截等。
- **测试验证结果**：列出 `go vet` 与 `go test` 的执行状态。

---

## 5. 常用构建与调试命令手册 (Command Reference)

所有命令均在项目根目录下执行：

```bash
# 1. 语法与静态规范检查
go vet ./...

# 2. 全量单元测试与 Mock 集成测试
go test -v ./...

# 3. 运行指定包或特定用例
go test -v ./config
go test -v ./deployer
go test -v ./web

# 4. 本地绿色单文件发布构建
# Windows 平台构建:
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o deploy.exe main.go
# Linux 平台构建:
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o deploy main.go

# 5. CLI 本地调试运行
go run . -c deploy.json -t api-server-01           # 过滤指定服务部署
go run . -c deploy.json -j 5                      # 指定最大并发度为 5
go run . -init                                    # 初始化生成配置样例

# 6. Web 控制台调试运行 (测试环境下可通过 DEPLOY_NO_OPEN=1 禁用浏览器自动唤起)
DEPLOY_NO_OPEN=1 go run . -web -addr 127.0.0.1:8080
```

---

## 6. 故障排查与防御矩阵 (Troubleshooting & Defense Matrix)

| 异常现象 | 根因排查方向 | 标准防御/修复策略 |
|---|---|---|
| **Windows 终端或 Web 日志出现中文乱码** | 命令子进程未启用代码页 65001 或输出包含非 UTF-8 GBK 字节。 | 检查命令是否被 `cmd.exe /C chcp 65001 >nul 2>&1 && <cmd>` 包装；确认输出读取是否经过 `decodeConsoleBytes`。 |
| **远程 Linux 目录创建出现反斜杠畸变** | 路径拼接使用了本地平台的 `filepath.Join` 而非 Posix 标准库 `path.Join`。 | 强制在所有远程目标路径计算中导入 `"path"`，仅本地路径使用 `"path/filepath"`。 |
| **取消任务后远程脚本仍在执行** | SSH Session 未正确捕获 Context 取消信号或未主动发送中断。 | 确保 `ssh.Session` 在 `ctx.Done()` 触发时被显式 `Close()`，阻断远端连接通道。 |
| **Web 触发部署报 HTTP 409 Conflict** | 属于预期的并发保护动作，当前已有部署任务正在运行。 | 检查 `isDeploying` 状态是否在部署结束（含 Panic/异常分支）时通过 `defer isDeploying.Store(false)` 释放。 |
| **大批服务部署时 SSH 连接报大量超时** | 并发过高导致本地 FD 枯竭或对端防刷流控触发。 | 检查是否通过 Worker Pool 信号量限制并发度，默认最大并发数建议设为 10。 |


