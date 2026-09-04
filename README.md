# 多服务器并发部署工具 (Multi-Service Deployer)

一款基于 **Go** 语言开发、**零运行依赖（无需安装 Node.js、Python 等环境）** 的多服务器自动化部署工具。
既支持在命令行终端运行，也内置了**开箱即用的可视化 Web 配置控制台**，支持多配置单元并行部署、SFTP 目录/文件流式上传、四阶段生命周期命令编排以及实时日志流。

---

## 🌟 核心特性

- ⚡ **运行零依赖**：编译为一个独立的绿色静态二进制可执行文件（`deploy.exe` / `deploy`，约 6MB），无需任何外部运行时依赖，随拷随用。
- 🖥️ **内置可视化 Web 控制台**：运行 `./deploy.exe -web` 即刻在浏览器中图形化编辑服务配置单元、增删复制、表单校验并**在线一键执行部署**。
- 🚀 **完全并发执行**：基于 Go Goroutine 原生并发调度，多台服务器同时部署，执行速度成倍提升。
- 🎨 **多色流式终端 / 网页实时日志**：并发执行时为每个服务节点分配专属终端颜色标签（如 `[api-server-01]`、`[web-server-02]`），网页端通过 SSE（Server-Sent Events）实时流式呈现。
- 📦 **内置 SFTP 文件同步**：支持递归上传目录或单个文件，支持通配符排除规则（如 `.git`, `node_modules`, `*.log`），自动创建远端层级目录。
- 🔗 **完整生命周期钩子体系**：
  1. 🌟 **`hooks.preDeploy`（全局批次前置）**：并发启动前在本地统一执行一次（如全局 `npm run build` 打包、代码检出），失败即直接阻断整个批次，绝不触碰远程服务器
  2. `preUploadLocal`：单个节点上传前本地执行命令
  3. 建立安全 SSH / SFTP 会话（支持密码、密钥及可选的 SHA256 指纹强校验）
  4. `preUploadRemote`：上传前远端执行命令（如停止老服务、备份数据、创建目录）
  5. SFTP 文件传输（内置高危根目录防清空保护）
  6. `postUploadRemote`：上传后远端执行命令（如赋权、解压、启动服务、健康检查）
  7. `postUploadLocal`：单个节点上传后本地执行命令
  8. 🌟 **`hooks.postDeploy`（全局批次后置）**：当且仅当全部选定节点成功部署后在本地执行一次（如发送企业微信通知、清理临时缓存）
- 📊 **可视化统计看板**：执行结束后自动输出汇总表格，直观展示每个服务器单元的状态（`SUCCESS` / `FAILED`）、耗时及错误详情。

---

## 🚀 快速上手

### 方式一：可视化 Web 控制台（推荐）

#### Windows 双击一键后台启停（极简运维）
- **`start.bat`**：双击即可在后台静默启动 Web 控制台，不留黑窗口，自动记录 PID 与日志（`deploy.log`），并自动调起浏览器访问 [http://127.0.0.1:8080](http://127.0.0.1:8080)；内置防重复启动检测。
- **`stop.bat`**：双击即可安全停止正在运行的后台服务并清理 PID 锁文件。

#### 命令行启动
```bash
./deploy.exe -web
```
- 程序将启动本地轻量 Web 服务（默认安全监听 `127.0.0.1:8080`），并**自动在系统默认浏览器中打开**。
- 可在页面上直观管理服务单元、全局批次前后钩子（`preDeploy` / `postDeploy`）、调整密码/路径/命令钩子、一键保存配置；
- 点击 **“⚡ 一键开始部署”** 按钮即可调出内置 Web 终端，实时观看多服务器并发部署进度与各节点彩色日志！

> 如需指定其他端口：`./deploy.exe -web -addr :9000`

---

### 方式二：命令行自动化部署

适合集成在 CI/CD 流水线或终端运维中：

```bash
# 1. 快速生成示例配置文件 deploy.example.json
./deploy.exe -init

# 2. 默认执行部署（自动读取当前目录下的 deploy.json 或 deploy.yaml，全并发运行）
./deploy.exe

# 3. 指定特定的配置文件
./deploy.exe -c ./custom-config.json

# 4. 指定仅部署某些服务单元（多个用逗号隔开）
./deploy.exe -t api-server-01,web-server-02

# 5. 临时切换为顺序串行执行（便于单步排查错误）
./deploy.exe -p=false
```

---

## 📝 配置文件规范 (`deploy.json` / `deploy.yaml`)

工具支持 JSON 与 YAML 两种格式，以配置单元为核心：

```json
{
  "parallel": true,
  "hooks": {
    "preDeploy": [
      "echo '==> [批次前置] 全局统一本地构建（仅执行一次）...'",
      "npm run build"
    ],
    "postDeploy": [
      "echo '==> [批次后置] 所有节点均部署成功（仅执行一次）...'"
    ]
  },
  "services": [
    {
      "name": "api-server-01",
      "server": {
        "host": "192.168.1.101",
        "port": 22,
        "username": "root",
        "password": "your_password",
        "privateKeyPath": "",
        "connectTimeout": 15
      },
      "upload": {
        "localPath": "./dist",
        "remotePath": "/opt/app/api",
        "exclude": [".git", "*.log", "node_modules", ".DS_Store"],
        "cleanRemote": false
      },
      "hooks": {
        "preUploadLocal": [
          "echo '==> 本地构建...'",
          "npm run build"
        ],
        "preUploadRemote": [
          "echo '==> 远端备份旧版本...'",
          "mkdir -p /opt/app/backup",
          "tar -czf /opt/app/backup/$(date +%s).tar.gz -C /opt/app api 2>/dev/null || true"
        ],
        "postUploadRemote": [
          "echo '==> 远端重启服务...'",
          "chmod +x /opt/app/api/run.sh",
          "systemctl restart my-api"
        ],
        "postUploadLocal": [
          "echo '==> api-server-01 本地通知发送完成'"
        ]
      }
    },
    {
      "name": "web-server-02",
      "server": {
        "host": "192.168.1.102",
        "port": 22,
        "username": "root",
        "password": "your_password"
      },
      "upload": {
        "localPath": "./web-dist",
        "remotePath": "/var/www/html",
        "exclude": [".git", "*.map"]
      },
      "hooks": {
        "preUploadLocal": "npm run build:web",
        "postUploadRemote": "nginx -s reload",
        "postUploadLocal": "echo 'web-server-02 部署成功'"
      }
    }
  ]
}
```

> 💡 **提示**：每个 hook 命令既支持单个字符串（如 `"npm run build"`），也支持字符串数组（如 `["cmd1", "cmd2"]`），解析器自动向下兼容。

---

## 🛠️ 命令行参数一览

| 参数 | 缩写 | 默认值 | 说明 |
|---|---|---|---|
| `-web` | - | 关闭 | 启动 Web 图形化配置控制台并自动打开浏览器 |
| `-addr <addr>` | - | `:8080` | Web 控制台监听地址，需配合 `-web` 使用（如 `:9000`） |
| `--config <file>` | `-c` | `deploy.json` / `deploy.yaml` | 指定配置文件路径 |
| `--target <names>`| `-t` | (全部服务) | 过滤仅部署指定的服务名称，逗号隔开 |
| `--parallel <bool>`| `-p` | `true` | 覆盖配置文件中的并发设置（`true` 并发，`false` 串行） |
| `-init` | - | - | 快速生成模板配置文件 `deploy.example.json` |
| `--version` | `-v` | - | 查看工具版本 |
| `--help` | `-h` | - | 查看完整帮助信息 |

---

## 🏗️ 跨平台编译说明

项目代码基于纯 Go 标准库与原生 Go 加密驱动，未启用 CGO，可在任何平台上一键交叉编译出目标系统的单一二进制：

```bash
# Windows x64 (生成 deploy.exe)
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o deploy.exe main.go

# Linux x64 (生成 deploy)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o deploy main.go

# macOS (Apple Silicon M1/M2/M3)
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w" -o deploy-mac main.go
```
生成的单文件内置了 Web 静态资源，无需附带任何额外 HTML/CSS 文件夹，单个文件即可独立运行全部功能。
