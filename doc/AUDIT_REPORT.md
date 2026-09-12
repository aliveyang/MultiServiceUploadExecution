# 多服务器并发部署工具 (MultiServiceUploadExecution) 综合审计报告

---

## 📑 报告概述与审计元数据

| 项目属性 | 审计内容 / 说明 |
|---|---|
| **项目名称** | 多服务器并发部署工具 (Multi-Service Deployer) |
| **所属仓库** | `MultiServiceUploadExecution` |
| **开发语言 / 版本** | Go 1.26+ (零外部运行时依赖，无 CGO 依赖) |
| **项目形态** | 跨平台单二进制 CLI 自动化部署工具 + 内置静态 Web 可视化控制台 |
| **审计日期** | 2026-09-04 |
| **审计维度** | 1. 架构设计与工程结构<br>2. 系统安全性与凭证保护<br>3. 并发性能与可靠性<br>4. 代码质量与可维护性<br>5. 单元测试与集成测试覆盖度 |
| **综合安全风险等级** | **中高危 (Medium-High)** （主要源于 Web 端无鉴权暴露及 SSH 弱校验） |
| **综合工程质量评分** | **78.5 / 100 (良好 / B+)** |

---

## 📊 总体审计评分看板

```
┌─────────────────────────────────────────────────────────────┐
│                       综合审计雷达                           │
├────────────────────────┬───────┬────────────────────────────┤
│ 审计维度               │ 得分  │ 评级   │ 核心评语          │
├────────────────────────┼───────┼───────┼────────────────────┤
│ 1. 架构设计与轻量化   │ 90/100│ A     │ 零依赖/单文件/设计敏捷 │
│ 2. 跨平台与编码兼容性 │ 92/100│ A+    │ 优秀解决 Windows 中文乱码│
│ 3. 部署生命周期编排   │ 88/100│ A     │ 5阶段模型清晰易懂   │
│ 4. 测试设计与自动化   │ 82/100│ B+    │ Mock SSH/SFTP 表现抢眼│
│ 5. 并发控制与资源保护 │ 70/100│ B-    │ 缺少 Worker 数量硬限制│
│ 6. 系统安全性与鉴权   │ 58/100│ C-    │ 缺少 Web 鉴权与指纹校验 │
├────────────────────────┴───────┴───────┴────────────────────┤
│ 综合评定：78.5 分 (B+ 级) - 具备优秀的工具型产品架构与交付水准，│
│          但面向公网或生产网络使用前，必须实施安全加固与鉴权。 │
└─────────────────────────────────────────────────────────────┘
```

---

## 🌟 核心设计优势与工程亮点

在审计过程中，该项目展现出多个非常优异的工程实践，值得肯定：

1. **极致的零依赖绿色单文件交付**
   - 源码纯基于 Go 标准库和核心加密驱动（`golang.org/x/crypto/ssh`、`pkg/sftp`），未启用 CGO（`CGO_ENABLED=0`）。
   - 通过 Go 1.16+ `embed.FS` 将前端 HTML/CSS/JS 静态资产全量打包进单一二进制可执行程序（约 6~8MB），部署无需 Node.js/Python 等环境，随拷随用。

2. **跨平台中文环境编码的深度优化**
   - 针对 Windows 控制台命令默认输出中文乱码（GBK/CP936 与 UTF-8 冲突）的业界痛点，在 `deployer/executor.go` 中采用了两级防御：
     1. 命令级强制切换代码页：`cmd.exe /C chcp 65001 >nul 2>&1 && <command>`；
     2. 数据流自动转码兜底：`decodeConsoleBytes` 实时检测有效 UTF-8，若遇非 UTF-8 字节自动调用 `simplifiedchinese.GBK.NewDecoder()` 转码为 UTF-8。
   - 保障了 Windows 与 Linux 之间中文输出的无缝兼容，Web 控制台日志不会产生乱码。

3. **高度解耦的五阶段生命周期编排模型**
   - `deployer/pipeline.go` 将部署过程严谨拆分为标准化流程：
     $$\text{本地预处理(PreLocal)} \rightarrow \text{SSH安全建连} \rightarrow \text{远端预处理(PreRemote)} \rightarrow \text{SFTP文件流传输} \rightarrow \text{远端后置(PostRemote)} \rightarrow \text{本地收尾(PostLocal)}$$
   - 阶段边界清晰，命令执行与文件传输职责分离，任一节点失败立即中止并输出精准耗时与错误链路。

4. **极为出色的仿真集成测试实践**
   - 在 `deployer/pipeline_integration_test.go` 中，未依赖外部不可靠的 SSH 服务器，而是通过 `crypto/rsa` 动态自签测试私钥，在测试生命周期内于内存临时启动标准 SSH/SFTP Mock 服务（基于 `golang.org/x/crypto/ssh` 与 `pkg/sftp`）。
   - 验证了端到端文件上传、权限设置、并发执行、错误阻断等核心闭环，保证了代码重构时的强韧性。

---

## 🚨 安全审计专项分析 (Security Audit)

经全面源码走读与威胁建模，发现以下主要安全隐患，按危害严重程度分级如下：

### 1. 【高危】Web 控制台默认全网段绑定且无鉴权机制（潜在远程代码执行）
- **风险位置**：`main.go:45`、`web/server.go:94-113`
- **代码片段**：
  ```go
  // main.go
  flag.StringVar(&webAddr, "addr", ":8080", "Web UI listen address")
  
  // web/server.go
  listener, err := net.Listen("tcp", s.addr) // 默认监听 0.0.0.0:8080
  mux.HandleFunc("/api/config", s.handleConfig)
  mux.HandleFunc("/api/deploy", s.handleDeploy)
  ```
- **漏洞危害**：
  1. `-addr` 默认值为 `:8080`，在不指定 IP 时直接绑定了宿主机的所有网络接口（`0.0.0.0`）。如果运行机器处于局域网、公共办公网或绑定了公网 IP，该端口即可被外部直接访问。
  2. `/api/config`（GET/POST）与 `/api/deploy`（POST）没有任何身份验证（Token、Session 或 BasicAuth）。
  3. 攻击者只需向 `/api/config` 发送 POST 请求，在 `hooks.preUploadLocal` 中注入任意系统命令，然后请求 `/api/deploy` 触发部署，即可直接以运行 `deploy.exe` 的本地用户权限在宿主机上执行任意系统命令（Remote Code Execution）。
- **整改建议**：
  - 默认监听地址应收紧为本地环回地址 `127.0.0.1:8080`，仅在用户显式指定 `-addr 0.0.0.0:8080` 时对外监听。
  - 增加 Web 控制台访问密码保护（可通过启动参数 `-token` 或 `-auth user:pass` 启用 HTTP Basic Auth / Bearer Token 验证）。

---

### 2. 【中高危】SSH 客户端全局禁用 HostKey 验证（中间人劫持风险）
- **风险位置**：`deployer/executor.go:114`
- **代码片段**：
  ```go
  sshConfig := &ssh.ClientConfig{
      User:            server.Username,
      Auth:            authMethods,
      HostKeyCallback: ssh.InsecureIgnoreHostKey(), // 默认忽略 HostKey 校验
      Timeout:         timeout,
  }
  ```
- **漏洞危害**：
  - `InsecureIgnoreHostKey()` 会盲目信任目标服务器返回的任何公钥。当部署任务跨越公网、云服务内网或不可信 Wi-Fi 环境时，易受到 ARP 欺骗、DNS 劫持或恶意路由发起的中间人（MitM）攻击，导致服务器密码或部署代码被窃取。
- **整改建议**：
  - 提供安全的 HostKey 校验策略：支持读取用户的 `~/.ssh/known_hosts` 文件（`knownhosts.New()`）。
  - 在配置文件 `ServerConfig` 中增加可选字段 `hostKeyFingerprint`，允许用户固定目标服务器的 SHA256 指纹；当两者均未指定时，输出安全警告。

---

### 3. 【中危】敏感凭据明文配置与 Web 接口明文回显
- **风险位置**：`config/config.go:50-58`、`web/server.go:117-133`
- **代码片段**：
  ```go
  // config/config.go
  type ServerConfig struct {
      Host           string `json:"host"`
      Password       string `json:"password"`
      PrivateKeyPath string `json:"privateKeyPath,omitempty"`
      Passphrase     string `json:"passphrase,omitempty"`
  }
  ```
- **漏洞危害**：
  1. `deploy.json` 文件以明文保存 SSH 密码和私钥 Passphrase，若不慎提交至公开 Git 仓库将导致凭据泄露。
  2. Web 界面请求 `GET /api/config` 时，后端直接将包含明文密码的完整 JSON 返回给前端浏览器，并填充至表单中，缺乏脱敏处理。
- **整改建议**：
  - 引入**环境变量插值支持**：允许在配置中使用 `${SERVER_PASSWORD}` 或 `${SSH_KEY_PATH}`，在加载配置时自动从系统环境中读取，避免硬编码。
  - Web 接口响应中对敏感字段进行脱敏（如输出 `******`），前端保存配置时如果字段内容未修改（仍为掩码）则保留原真实值。

---

### 4. 【中危】`cleanRemote: true` 缺乏危险根路径校验
- **风险位置**：`deployer/sftp_uploader.go:69-73`
- **代码片段**：
  ```go
  if cfg.CleanRemote {
      u.log.Info("Cleaning remote path: %s", remotePath)
      _ = u.sftpClient.RemoveAll(remotePath)
  }
  ```
- **漏洞危害**：
  - 若用户在配置文件中疏忽将 `remotePath` 配置为根目录 `/`、`/var`、`/etc` 或 `/usr`，在开启 `cleanRemote: true` 的情况下，SFTP 客户端会递归删除远端服务器的根目录或系统核心目录，造成不可逆的灾难性生产事故。
- **整改建议**：
  - 在 `config.ValidateAndNormalize` 或执行清理前增加高危路径黑名单校验，严格禁止对 `/`、`/*`、`/root`、`/etc`、`/bin`、`/usr`、`/home` 等关键路径执行递归删除。

---

## ⚙️ 架构设计与高并发可靠性审计 (Architecture & Concurrency)

### 1. 并发无限制风险（缺少 Worker Pool / 信号量控制）
- **现象**：
  在 `deployer/manager.go:52-60` 中：
  ```go
  for i, svc := range services {
      wg.Add(1)
      go func(idx int, s config.ServiceConfig) {
          defer wg.Done()
          results[idx] = RunServicePipeline(s, idx)
      }(i, svc)
  }
  wg.Wait()
  ```
- **影响分析**：
  当配置的服务节点数量较多（如 50~100+ 台集群）且开启 `parallel: true` 时，程序会无节制瞬间派生等量 Goroutine。这将导致本地网络文件描述符（Socket FD）、并发 SSH 握手量激增，可能触发系统的 `too many open files` 限制、耗尽本地临时端口或造成网络阻塞。
- **建议**：
  引入带并发限制的 Worker Pool 或信号量（`make(chan struct{}, maxConcurrency)`），支持用户通过命令行参数 `-j 10` 指定最大并行工作线程数（默认建议 8~16）。

---

### 2. 全生命周期缺乏 `context.Context` 取消与超时传播
- **现象**：
  从 `main.go` 到 `DeployManager.Run()`、`RunServicePipeline()`，全链路均未传入 `context.Context`。
- **影响分析**：
  - 当运维人员在终端按下 `Ctrl+C` 发送中断信号，或 Web 用户中途关闭页面时，已派生的本地子进程与远程 SSH/SFTP 传输无法感知取消事件，仍然会在后台持续执行直至出错或结束。
  - 单步命令执行（如 `ExecuteLocalCommand`）未设置超时时间，若某个构建命令发生死锁挂起，整个部署流水线将无限期卡死。
- **建议**：
  在主入口监听系统信号（`signal.NotifyContext`），并将 `ctx context.Context` 贯穿传递至 `exec.CommandContext`、`ssh.Client` 以及每阶段流水线中。

---

### 3. Web 部署状态缺乏并发互斥与重入保护
- **现象**：
  在 `web/server.go:167-205` 中，每次接收到 `POST /api/deploy` 请求，后端都会无条件通过 `go func() { mgr.Run() }()` 启动一个新的部署实例。
- **影响分析**：
  如果用户在页面上多次快速点击“一键部署”，多个独立的部署线程将同时操作同一批目标服务器，引发远端文件读写冲突和命令竞态。
- **建议**：
  在 `web.Server` 中维护一个原子状态锁（如 `atomic.Bool isDeploying`），当已有部署正在运行时，对新的触发请求返回 `409 Conflict` 或 `Deployment is already running`。

---

### 4. SSE 广播设计及日志背压考量
- **现象**：
  `web/server.go` 中 `msgChan := make(chan string, 100)`，广播时使用非阻塞发送：
  ```go
  select {
  case ch <- msg:
  default:
  }
  ```
- **影响分析**：
  当编译或大文件上传产生海量高频日志时，若前端渲染或网络读取较慢导致缓冲区占满（100 行），未及时消费的日志行会被 `default` 分支直接丢弃，导致 Web 终端日志呈现出现内容丢失或断层。
- **建议**：
  适当调大通道缓冲（如 500~1000），或在缓冲区满时进行日志聚合压缩，保障日志完整性。

---

## 🔍 代码规范、测试完备性与依赖审计 (Quality & Tests)

### 1. 自动化测试现状评估
当前各模块测试执行全部通过（PASS），耗时约 4 秒，覆盖度统计如下：

| 包模块 | 语句覆盖率 | 测试类型与评价 |
|---|---|---|
| `config` | **63.2%** | 覆盖 JSON/YAML 单值与数组格式反序列化、必填项校验等 |
| `deployer` | **76.1%** | 质量极高，包含完整的自签 RSA + Mock SSH/SFTP 端到端集成测试 |
| `logger` | **60.0%** | 包含多协程并发写安全与颜色转义测试 |
| `web` | **15.7%** | 仅覆盖静态文件访问与配置 API 测试，未覆盖 SSE 流与启动逻辑 |

### 2. 依赖管理状况
项目仅引入 6 个轻量级官方/社区成熟库，`go.sum` 校验完整，无任何已知高危 CVE 依赖：
- `golang.org/x/crypto` (v0.55.0) - SSH 客户端与签名实现
- `github.com/pkg/sftp` (v1.13.11) - SFTP 子系统驱动
- `golang.org/x/text` (v0.41.0) - GBK/UTF-8 字符集转换
- `gopkg.in/yaml.v3` (v3.0.1) - YAML 配置解析
- `golang.org/x/sys` (v0.47.0) - 系统调用支持

---

## 🛠️ 优先级加固建议与落地代码方案

### 【P0 - 立即修复】修复 Web 控制台网络监听绑定
将 `main.go` 中默认监听地址由 `:8080` 改为明确的环回接口：
```diff
--- a/main.go
+++ b/main.go
@@ -45,1 +45,1 @@
-       flag.StringVar(&webAddr, "addr", ":8080", "Web UI listen address, used with -web (e.g. :9000)")
+       flag.StringVar(&webAddr, "addr", "127.0.0.1:8080", "Web UI listen address, used with -web (e.g. 127.0.0.1:8080)")
```

### 【P1 - 建议实施】并发 Worker Pool 限流实现
在 `deployer/manager.go` 中引入工作池控制，避免网络资源瞬间耗尽：
```go
// 并发工作池示例
maxWorkers := 10 // 可由配置或命令行参数指定
semaphore := make(chan struct{}, maxWorkers)

for i, svc := range services {
    wg.Add(1)
    go func(idx int, s config.ServiceConfig) {
        defer wg.Done()
        semaphore <- struct{}{}        // 获取令牌
        defer func() { <-semaphore }() // 释放令牌
        results[idx] = RunServicePipeline(s, idx)
    }(i, svc)
}
wg.Wait()
```

### 【P1 - 建议实施】高危目录清理防护
在 `config/config.go` 校验逻辑中增加黑名单校验：
```go
var dangerousPaths = map[string]bool{
    "/": true, "/root": true, "/bin": true, "/boot": true,
    "/dev": true, "/etc": true, "/home": true, "/lib": true,
    "/opt": true, "/usr": true, "/var": true,
}

func isDangerousPath(p string) bool {
    clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
    return dangerousPaths[clean]
}
```

### 【P2 - 建议实施】全流程 Context 支持
为 `ExecuteLocalCommand` 和 `ExecuteRemoteCommand` 补充 Context 超时支持，使中断信号可迅速传递，防止僵尸进程。

---

## 📋 审计结论

**多服务器并发部署工具 (MultiServiceUploadExecution)** 是一款架构精巧、开发规范、用户体验极佳的绿色部署工具。在工程实现上，其自研的零依赖架构、跨平台编码自适应、以及集成测试中精湛的 Mock SSH 设计表现出了很高的工业级水准。

当前主要风险集中在 **Web 管理端默认暴露与缺少身份鉴权**、以及 **SSH HostKey 缺省全信任** 两个网络安全维度。在将默认监听地址限制为本地环回、增加访问鉴权与并发流控后，该项目完全具备投入正式生产运维环境的综合质量要求。

---

## 🏁 审计问题加固与修复闭环记录 (2026-09-04 已实施闭环)

经过针对性重构与修复，审计报告中列出的主要缺陷已全部得到工程级修复并完成闭环验证：

| 缺陷编号 | 缺陷项 | 修复方案与落地点 | 验证状态 |
|---|---|---|---|
| **SEC-01** | Web 控制台全网段暴露风险 | `main.go` 默认监听地址由 `:8080` 改为本地回环 `127.0.0.1:8080`，阻断外部非授权探测 | ✅ **已闭环** |
| **SEC-02** | SSH 客户端 HostKey 校验缺失 | `config.ServerConfig` 新增 `HostKeyFingerprint` 字段；`deployer/executor.go` 引入 SHA256 指纹强校验 | ✅ **已闭环** |
| **SEC-03** | 敏感密码私钥明文硬编码风险 | `config/config.go` 增加 `ExpandEnvVariables`，凭证支持 `${ENV_VAR}` 占位符从宿主环境动态注入 | ✅ **已闭环** |
| **SEC-04** | `cleanRemote: true` 误删系统目录 | `config.IsDangerousRemotePath` 建立核心根路径黑名单，并在 `config` 静态校验与 `sftp_uploader` 动态执行前双重阻断 | ✅ **已闭环** |
| **SEC-05** | Web 端接口明文泄露及 `${ENV}` 覆盖 | `config.MaskConfig` 脱敏返回 `******`；`config.MergePreservingSecrets` 在保存时智能保留原始密码与环境变量占位符 | ✅ **已闭环** |
| **ARCH-01** | 并发无限制压垮本地 FD/Socket | `deployer/manager.go` 引入带有信号量容量的 Worker Pool，支持通过 `-j` 参数限流（默认 10） | ✅ **已闭环** |
| **ARCH-02** | 缺少 Context 取消机制 | `signal.NotifyContext` 监听中断信号，全链路穿透 `ctx`，支持本地命令及远程命令超时中断与退出 | ✅ **已闭环** |
| **ARCH-03** | Web 部署重复触发并发冲突 | `web/server.go` 引入 `isDeploying atomic.Bool` 防重入锁，任务运行中返回 HTTP 409 Conflict | ✅ **已闭环** |
| **ARCH-04** | SSE 高频日志溢出丢包 | SSE 消息缓冲队列由 100 扩容至 1024，降低瞬时高密日志下的丢包概率 | ✅ **已闭环** |
| **ARCH-05** | 远端高危路径计算平台语义偏差 | `config.IsDangerousRemotePath` 强制使用 POSIX 标准库 `path.Clean`，防御 Windows 平台下反斜杠及相对路径越权逃逸 | ✅ **已闭环** |
| **ARCH-06** | Web 端缺少任务中断与优雅停机 | `web.Server` 支持 `StartContext` 优雅停机，新增 `POST /api/deploy/cancel` 主动中断与前端一键中止按钮 | ✅ **已闭环** |
| **ARCH-07** | 单一流水线无法支撑多场景与分阶段发布 | 引入 `ScenarioConfig`、`Group`、`Type (standard/exec_only/sync_only)` 与 `Stage` 波次编排；实现跨阶段熔断机制，前置阶段失败自动阻断后续波次 | ✅ **已闭环** |
| **ARCH-08** | 全局批次生命周期缺乏分组独立性 | 引入 `GroupConfig` 与 `BatchHooks`，支持每个业务分组拥有独占的 `preDeploy` / `postDeploy` 钩子，仅在涉及该组时在本地触发 | ✅ **已闭环** |
| **SEC-06** | Windows Shell 单引号命令重定向逃逸隐患 | 修复示例命令中由于单引号界定符在 cmd.exe 下被识别为输出重定向的缺陷，强制统一使用双引号或安全转义输出 | ✅ **已闭环** |

**最终验证结论**：全模块单元测试与端到端 Mock SSH 仿真测试 100% 通过（PASS），绿色单二进制 `deploy.exe` 已成功构建并验证完毕。

---

## 🔄 前后端对齐改造安全增补 (2026-09-12)

Web 控制台完成全新 UI 前后端对齐改造（单文件 SPA 全面接通后端），随改造引入的存储与接口能力同步完成安全评估与加固：

| 编号 | 事项 | 加固方案与落地点 | 状态 |
|---|---|---|---|
| **SEC-07** | 新增 SSH 私钥库的密钥泄露风险 | `web/keys.go` 私钥以 0600 权限存储于 `workspaces/<空间>/keys/`；列表/导入/删除响应仅含算法与 SHA256 公钥指纹，**任何接口均不回传私钥内容**；文件名经 `keyNamePattern` 白名单校验防路径穿透 | ✅ **已闭环** |
| **SEC-08** | 批次历史归档的敏感信息暴露 | 批次记录 JSON 仅含节点状态/耗时/错误摘要，日志归档沿用既有 SSE 广播口径（脱敏后日志行）；归档目录按工作空间物理隔离 | ✅ **已闭环** |
| **SEC-09** | settings.json 监听地址弱化默认绑定 | `config.Settings.Validate` 强制 host:port 格式校验；默认绑定红线保持 `127.0.0.1:8080`，settings 值仅在用户未显式指定 `-addr` 时于启动时生效 | ✅ **已闭环** |
| **ARCH-09** | 批次结果无结构化输出（前端只能解析文本日志） | `deployer/batch.go` 引入批次生命周期结构化事件（`OnEvent` 回调，CLI 场景零开销）；SSE 在文本日志外新增命名 JSON 事件；`web/history.go` 事件收集器聚合落盘，端到端测试覆盖事件序列 | ✅ **已闭环** |
| **ARCH-10** | Web 包单文件膨胀、职责耦合 | `web/server.go` 瘦身为装配层，handler 按资源域拆分为 `sse.go` / `workspace_handlers.go` / `config_handlers.go` / `deploy_handlers.go` / `system.go` / `history.go` / `keys.go` / `settings.go`；依赖方向保持单向 `config ← deployer ← web` | ✅ **已闭环** |

**回归验证结论**：`go vet ./...` 0 警告；`go test ./...` 全部 PASS（含新增的 history 落盘/端点、密钥 CRUD 与不泄漏断言、设置读写/409/校验、批次事件序列用例）；`CGO_ENABLED=0` 构建自检通过。

## 🔄 遗留项专项闭环 (2026-09-12 第二批)

| 编号 | 事项 | 方案与落地点 | 状态 |
|---|---|---|---|
| **ARCH-11** | 独立主机库（此前仅能从 services[].server 派生） | `config.DeployConfig` 新增 `hosts[]`（`HostConfig`: name + server），服务通过 `hostRef` 引用并以"内联非零字段优先"语义解析合并；`ValidateAndNormalize` 校验主机名唯一/凭据齐全/悬空引用；`MaskConfig`/`MergePreservingSecrets`/`ExpandEnvVariables` 全部覆盖主机库凭证（掩码红线同步生效） | ✅ **已闭环** |
| **ARCH-12** | SSE 断线丢日志不可恢复 | SSE 消息分配全局单调 `seq` 并维护 4096 条环形缓冲；`handleSSE` 支持 `Last-Event-ID` 断点重放（EventSource 自动重连原生携带），重连后日志与结构化事件自动补发 | ✅ **已闭环** |
| **ARCH-13** | 活动工作空间重启后丢失（每次重启回落 default） | `settings.json` 新增 `activeWorkspace`；工作空间 select/create/delete 时落盘，`NewServer` 启动时校验存在性并恢复 | ✅ **已闭环** |

## 🔄 启动测试与真实浏览器实测修复闭环 (2026-09-12 第三批)

交付前以内置浏览器模拟真实用户全流程操作（配置编辑/保存、主机库抽屉、部署触发、SSE 实时终端、历史详情、设置保存、空间切换），实测发现并修复以下缺陷：

| 编号 | 缺陷 | 根因与修复 | 验证 |
|---|---|---|---|
| **ARCH-14** | 批次归档触发 SSE 自死锁：首个批次结束后 `isDeploying` 永久为 true，后续部署全部 409、取消端点挂起 | `batchCollector.observe` 持有 `c.mu` 期间 `persistLocked` 调用 logger → `OnLog` → `teeLog` → `appendLog` 再次请求 `c.mu`，非重入互斥锁自死锁。重构锁模型：批次记录仅由部署 goroutine 串行访问（免锁），`logMu` 仅保护日志文件句柄，并约定持锁期间禁止调用 logger。单元测试未覆盖的原因：直调 handler 时 `logger.OnLog` 未接线。新增 `TestDeployTeeLogNoDeadlock` 回归测试（接通真实日志回调跑完整部署，断言锁释放） | ✅ 已闭环（浏览器实测 + 回归测试） |
| **ARCH-15** | 前端部署触发竞态：`batch_started` 事件先于 POST 响应到达时，`state.deploy` 被响应处理清空，执行页进度不显示 | `startDeployFlow` 改为在发起请求前清理上一批次状态 | ✅ 已闭环（实测部署 ID/节点状态/进度全链路渲染） |
| **ARCH-16** | 前端后台标签页更新停滞：`softRender` 仅依赖 rAF，后台节流下 SSE 驱动的界面冻结 | rAF 之外增加 setTimeout 兜底（渲染幂等守卫） | ✅ 已闭环 |
| **SEC-10** | `settings.json` 被误识别为工作空间 "settings"；同名工作空间可创建并与运行时文件冲突 | `ListWorkspaces` 排除运行时设置文件；`IsValidWorkspaceID` 增加 `settings` 保留名拒绝 | ✅ 已闭环 |
| **ARCH-17** | 抽屉为动态注入 DOM，`bind()` 渲染期绑定使其内部按钮无事件 | 抽屉打开后调用 `bindDrawerActions()` 单独绑定 | ✅ 已闭环（主机库抽屉编辑→保存→落盘实测） |
