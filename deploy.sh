#!/bin/bash
# ---------------------------------------------------------
# 修复 Windows Git Bash 下路径自动转换导致的问题
export MSYS_NO_PATHCONV=1
# ---------------------------------------------------------

# QzoneWall-Go Docker Compose 部署脚本

set -e

echo "🚀 开始部署 QzoneWall-Go (Docker Compose 版)..."

# 1. 检查 Docker 和 Docker Compose
if ! command -v docker &> /dev/null; then
    echo "❌ Docker 未安装"
    exit 1
fi

# 检查是使用 'docker compose' (新版) 还是 'docker-compose' (旧版)
if docker compose version &> /dev/null; then
    DOCKER_COMPOSE_CMD="docker compose"
elif command -v docker-compose &> /dev/null; then
    DOCKER_COMPOSE_CMD="docker-compose"
else
    echo "❌ 未找到 Docker Compose 插件或命令"
    exit 1
fi

# 2. 目录处理 (重点强调)
WORK_DIR="wall"
if [ ! -d "$WORK_DIR" ]; then
    echo "📂 创建工作目录: $WORK_DIR"
    mkdir -p "$WORK_DIR"
fi

# !!! 关键步骤：进入目录 !!!
cd "$WORK_DIR"
echo "📂 已进入工作目录: $(pwd)"
echo "⚠️  接下来的所有操作都将在该目录下执行"

# 3. 清理旧进程 (新增功能)
echo "🧹 正在检查并清理旧服务..."

# 尝试通过 Compose 停止
$DOCKER_COMPOSE_CMD down 2>/dev/null || true

# 双重保险：检查是否有同名容器（防止之前是用 docker run 手动启动的）
if docker ps -a --format '{{.Names}}' | grep -q "^qzonewall$"; then
    echo "   ⚠️ 发现旧的 qzonewall 容器实例，正在强制删除..."
    docker rm -f qzonewall
else
    echo "   ✅ 无残留旧容器"
fi

# 4. 创建必要目录与权限控制
if [ ! -d "data" ]; then
    echo "📁 创建数据目录 data/ ..."
    mkdir -p data
    chmod 777 data
fi

if [ ! -d "uploads" ]; then
    echo "📁 创建图片目录 uploads/ ..."
    mkdir -p uploads
    chmod 777 uploads
fi

# 5. 创建配置文件 (如果不存在)
# ⚠️ 必须在启动容器前确保 config.yaml 是个文件
if [ ! -f "config.yaml" ]; then
    echo "📝 生成 config.yaml..."
    cat > config.yaml << 'EOF'
# QzoneWall-Go 配置文件

qzone:
  keep_alive: 10s
  max_retry: 2
  timeout: 30s

bot:
  zero:
    nickname: ["表白墙", "墙墙"]
    command_prefix: "/"
    super_users: [123456789] # ⚠️ 修改这里
    ring_len: 4096
    latency: 1000000
    max_process_time: 240000000000
  ws:
    - url: "ws://localhost:3001" # ⚠️ 修改这里
      access_token: "your_token"   # ⚠️ 修改这里
  manage_group: 0

wall:
  show_author: false
  anon_default: false
  max_images: 9
  max_text_len: 2000
  publish_delay: 0s

database:
  path: "data/data.db"

web:
  enable: true
  addr: ":8081"
  admin_user: "admin"
  admin_pass: "admin123" # ⚠️ 修改这里

censor:
  enable: true
  words: ["广告", "代写"]
  words_file: ""

worker:
  workers: 1
  retry_count: 3
  retry_delay: 5s
  rate_limit: 30s
  poll_interval: 5s

log:
  level: "info"
EOF
    echo "✅ 配置文件已创建"
else
    echo "ℹ️  配置文件已存在 (跳过创建)"
fi

# 6. 生成 docker-compose.yml
echo "📝 生成/更新 docker-compose.yml..."
cat > docker-compose.yml <<EOF
services:
  qzonewall:
    image: guohuiyuan/qzonewall-go:latest
    container_name: qzonewall
    restart: unless-stopped
    ports:
      - "8081:8081"
    volumes:
      - ./config.yaml:/home/appuser/config.yaml
      - ./data:/home/appuser/data
      - ./uploads:/home/appuser/uploads
    environment:
      - TZ=Asia/Shanghai
EOF

# 7. 启动服务
echo "📦 拉取最新镜像..."
$DOCKER_COMPOSE_CMD pull

echo "🏃 启动容器 (在 $WORK_DIR 目录下)..."
$DOCKER_COMPOSE_CMD up -d

# 8. 检查状态
echo "⏳ 等待初始化 (3秒)..."
sleep 3

if docker ps | grep -q "qzonewall"; then
    echo ""
    echo "✅ 部署成功！"
    echo "------------------------------------------------"
    echo "🌐 管理后台: http://localhost:8081"
    echo ""
    echo "👇 常用维护命令 (请务必先进入 wall 目录):"
    echo "   cd $(pwd)"
    echo "   查看日志: $DOCKER_COMPOSE_CMD logs -f"
    echo "   停止服务: $DOCKER_COMPOSE_CMD down"
    echo "   重启服务: $DOCKER_COMPOSE_CMD restart"
    echo "------------------------------------------------"
else
    echo ""
    echo "❌ 容器启动失败！"
    echo "请运行以下命令查看错误日志："
    echo "cd $(pwd) && $DOCKER_COMPOSE_CMD logs"
    exit 1
fi