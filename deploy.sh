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

# 4. 创建必要目录
# 数据目录
if [ ! -d "data" ]; then
    echo "📁 创建数据目录 data/ ..."
    mkdir -p data
    chmod 777 data
fi

# [新增] 图片上传目录
if [ ! -d "uploads" ]; then
    echo "📁 创建图片目录 uploads/ ..."
    mkdir -p uploads
    # 赋予权限防止上传失败
    chmod 777 uploads
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

# [修改] 挂载 uploads 目录
docker run -d \
  --name "$CONTAINER_NAME" \
  --restart unless-stopped \
  -p 8081:8081 \
  -v "$(pwd)/config.yaml://home/appuser/config.yaml" \
  -v "$(pwd)/data://home/appuser/data" \
  -v "$(pwd)/uploads://home/appuser/uploads" \
  guohuiyuan/qzonewall-go:latest

# 8. 检查状态
echo "⏳ 等待初始化 (3秒)..."
sleep 3

if docker ps | grep -q "$CONTAINER_NAME"; then
    echo ""
    echo "✅ 部署成功！"
    echo "------------------------------------------------"
    echo "📂 工作目录: $(pwd)"
    echo "🌐 管理后台: http://localhost:8081"
    echo "👤 默认账号: admin / admin123 (请在配置中修改)"
    echo "------------------------------------------------"
    echo "📊 查看日志: docker logs -f $CONTAINER_NAME"
    echo "🛑 停止服务: docker stop $CONTAINER_NAME"
    echo "🔄 重启服务: docker restart $CONTAINER_NAME"
    echo "------------------------------------------------"
    echo "⚠️  提示：如果你在新的终端操作，请先进入目录："
    echo "    cd wall"
else
    echo ""
    echo "❌ 容器启动失败！"
    echo "请运行以下命令查看错误日志："
    echo "docker logs $CONTAINER_NAME"
    exit 1
fi