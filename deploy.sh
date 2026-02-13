#!/bin/bash
# ---------------------------------------------------------
# 修复 Windows Git Bash 下路径自动转换导致的问题
export MSYS_NO_PATHCONV=1
# ---------------------------------------------------------

# QzoneWall-Go Docker 部署脚本

set -e

echo "🚀 开始部署 QzoneWall-Go..."

# 1. 检查 Docker
if ! command -v docker &> /dev/null; then
    echo "❌ Docker 未安装"
    exit 1
fi

# 2. 目录处理
WORK_DIR="wall"
if [ ! -d "$WORK_DIR" ]; then
    mkdir -p "$WORK_DIR"
fi
cd "$WORK_DIR"

# 3. 拉取镜像
echo "📦 拉取 Docker 镜像..."
docker pull guohuiyuan/qzonewall-go:latest

# 4. 创建 data 目录 (关键修改：使用文件夹而不是单文件)
if [ ! -d "data" ]; then
    echo "📁 创建数据目录 data/ ..."
    mkdir -p data
    # 给该目录赋予宽泛权限，确保容器内非 root 用户能写入
    chmod 777 data
fi

# 5. 创建配置文件
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
  # [关键修改] 数据库路径指向挂载目录内部
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
    echo "ℹ️  配置文件已存在"
fi

# 6. 停止旧容器
CONTAINER_NAME="qzonewall"
docker stop "$CONTAINER_NAME" >/dev/null 2>&1 || true
docker rm "$CONTAINER_NAME" >/dev/null 2>&1 || true

# 7. 运行新容器
echo "🏃 启动新容器..."

# 注意：这里挂载的是 data 目录，而不是 data.db 文件
docker run -d \
  --name "$CONTAINER_NAME" \
  --restart unless-stopped \
  -p 8081:8081 \
  -v "$(pwd)/config.yaml://home/appuser/config.yaml" \
  -v "$(pwd)/data://home/appuser/data" \
  guohuiyuan/qzonewall-go:latest

# 8. 检查状态
echo "⏳ 等待初始化 (3秒)..."
sleep 3

if docker ps | grep -q "$CONTAINER_NAME"; then
    echo "✅ 部署成功！日志如下："
    docker logs --tail 5 $CONTAINER_NAME
    
    echo ""
    echo "--------------------------------------------------------"
    echo "🔔  重要提示："
    echo "    所有的配置和数据都在 'wall' 目录中。"
    echo "    如果你需要修改配置文件或查看数据，请务必先进入目录："
    echo ""
    echo "    👉  cd wall"
    echo "--------------------------------------------------------"
else
    echo "❌ 启动失败，查看日志："
    docker logs $CONTAINER_NAME
fi