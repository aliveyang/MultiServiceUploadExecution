#!/usr/bin/env bash
# ============================================================
# 开发热重载脚本（Git Bash / Linux / macOS 通用）
#   - 前端：以 DEPLOY_DEV=1 启动，Web 控制台直读磁盘 web/static，
#           修改 index.html 后刷新浏览器即生效，无需重启
#   - 后端：监听 *.go / go.mod / go.sum 变更，自动重编译并重启服务；
#           编译失败时保持旧进程继续运行，修复后保存文件即自动重试
# 用法: ./dev.sh [deploy 参数...]
#   例: ./dev.sh                          # 默认 127.0.0.1:8080
#       ./dev.sh -addr 127.0.0.1:8081     # 指定监听地址
# ============================================================
set -u
cd "$(dirname "$0")"

export DEPLOY_DEV=1
export DEPLOY_NO_OPEN=1   # 每次自动重启不重复弹出浏览器，请手动访问打印的地址
BIN="deploy-dev.exe"

# 最新源码修改时间（Go 源码 + 模块文件），作为变更检测指纹
src_mtime() {
  {
    find main.go config deployer logger web -name '*.go' -printf '%T@\n' 2>/dev/null
    stat -c '%Y' go.mod go.sum 2>/dev/null
  } | sort -nr | head -n1
}

build() {
  CGO_ENABLED=0 go build -o "$BIN" main.go
}

PID=""
cleanup() {
  if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    sleep 0.3
    kill -9 "$PID" 2>/dev/null || true
  fi
  rm -f "$BIN"
}
trap cleanup EXIT INT TERM

last_mtime=$(src_mtime)
need_start=true

# 开发脚本默认启动 Web 控制台；用户显式传了 -web 时不重复添加
ARGS=("$@")
has_web=false
for a in "${ARGS[@]:-}"; do
  [[ "$a" == "-web" ]] && has_web=true
done
$has_web || ARGS=("-web" "${ARGS[@]}")

while true; do
  cur_mtime=$(src_mtime)
  if [[ "$cur_mtime" != "$last_mtime" ]]; then
    last_mtime="$cur_mtime"
    need_start=true
  fi

  if $need_start; then
    need_start=false
    if build; then
      if [[ -n "$PID" ]] && kill -0 "$PID" 2>/dev/null; then
        echo "[dev] 源码变更，重启服务..."
        kill "$PID" 2>/dev/null || true
        for _ in $(seq 1 20); do
          kill -0 "$PID" 2>/dev/null || break
          sleep 0.1
        done
        kill -9 "$PID" 2>/dev/null || true
        wait "$PID" 2>/dev/null
      fi
      echo "[dev] 启动 Web 服务: $BIN ${ARGS[*]}"
      "./$BIN" "${ARGS[@]}" &
      PID=$!
    else
      echo "[dev] 编译失败，保持当前服务运行；修复后保存任意源码文件自动重试"
    fi
  else
    # 服务意外退出（如端口被占用/panic）时给出提示，等待下次源码变更再拉起
    if [[ -n "$PID" ]] && ! kill -0 "$PID" 2>/dev/null; then
      wait "$PID" 2>/dev/null
      echo "[dev] 服务进程已退出 (exit=$?)，等待源码变更后自动重启"
      PID=""
    fi
  fi
  sleep 1
done
