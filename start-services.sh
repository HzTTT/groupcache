#!/bin/bash

# 停止已有的服务
echo "停止已有的服务..."
pkill -f "go run internal/sourceapp/cmd/main.go" 2>/dev/null
pkill -f "go run internal/app/main.go" 2>/dev/null

# 等待服务完全停止
sleep 2

# 设置变量
SOURCEAPP_PORT=8086
API_PORT=8080
GROUPCACHE_PORT=8081
DATA_DIR="./data"

# 创建数据目录
mkdir -p $DATA_DIR

# 确定内网IP
# 获取本机内网IP (Linux 和 macOS 支持)
get_ip() {
    local IP=""
    
    # 尝试使用 hostname 命令
    if command -v hostname >/dev/null 2>&1; then
        IP=$(hostname -I 2>/dev/null | awk '{print $1}')
    fi
    
    # 如果不成功，尝试使用 ifconfig 命令 (macOS 和一些 Linux 系统)
    if [ -z "$IP" ] && command -v ifconfig >/dev/null 2>&1; then
        # 尝试找到真实的网络接口 (非 lo, docker, vmware 等)
        IP=$(ifconfig | grep "inet " | grep -v 127.0.0.1 | grep -v docker | grep -v vmnet | awk '{print $2}' | head -n 1)
        
        # macOS 上可能需要去掉 inet 前缀
        IP=${IP#inet}
        # 去掉可能的冒号前缀 (某些系统的格式)
        IP=${IP#addr:}
    fi
    
    # 如果仍然不成功，使用默认值
    if [ -z "$IP" ]; then
        IP="localhost"
        echo "无法确定内网IP，使用 localhost"
    else
        echo "使用内网IP: $IP"
    fi
    
    echo "$IP"
}

SELF_HOST=$(get_ip)

# 启动数据源服务
echo "启动数据源服务在 $SELF_HOST:$SOURCEAPP_PORT..."
cd "$(dirname "$0")"
go run internal/sourceapp/cmd/main.go -db "$DATA_DIR/sqlite.db" -http ":$SOURCEAPP_PORT" &
SOURCEAPP_PID=$!

# 等待数据源服务启动
echo "等待数据源服务启动..."
sleep 3

# 启动缓存服务
echo "启动缓存服务在 $SELF_HOST:$API_PORT (API) 和 $SELF_HOST:$GROUPCACHE_PORT (Groupcache)..."
SELF_HOST=$SELF_HOST \
SOURCEAPP_SERVICE_URL="http://$SELF_HOST:$SOURCEAPP_PORT" \
API_PORT=$API_PORT \
GROUPCACHE_PORT=$GROUPCACHE_PORT \
go run internal/app/main.go &
APP_PID=$!

echo "服务已启动:"
echo "- 数据源服务 (PID: $SOURCEAPP_PID): http://$SELF_HOST:$SOURCEAPP_PORT"
echo "- 缓存服务 API (PID: $APP_PID): http://$SELF_HOST:$API_PORT"
echo "- 缓存服务 Groupcache: http://$SELF_HOST:$GROUPCACHE_PORT"
echo ""
echo "按 Ctrl+C 停止所有服务"

# 捕获中断信号，优雅地关闭服务
trap "echo '正在停止服务...'; kill $SOURCEAPP_PID $APP_PID 2>/dev/null; exit" INT TERM

# 等待子进程
wait 