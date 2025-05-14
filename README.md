# Groupcache 分布式缓存系统

这是一个基于 golang/groupcache 的分布式缓存系统，由以下几个主要组件组成：

1. **Groupcache 缓存服务**: 核心缓存服务，实现了分布式缓存功能
2. **Sourceapp 数据服务**: 基于SQLite的后端数据存储服务，通过HTTP API提供数据

## 系统架构

- `internal/app`: 主应用目录，包含缓存服务实现
- `internal/sourceapp`: 数据源服务，基于SQLite提供HTTP API数据服务

系统采用解耦设计，缓存服务通过HTTP API访问数据源服务，而不是直接嵌入数据库。

## 快速启动

使用提供的启动脚本可以快速启动整个系统：

```bash
# 给脚本添加执行权限
chmod +x start-services.sh

# 启动服务
./start-services.sh
```

脚本会自动检测您的内网IP地址，并使用它来配置服务。这样可以确保在局域网内的其他机器能够正确访问您的服务。

## 手动启动服务

### 1. 启动数据源服务

数据源服务基于SQLite，提供数据存储和访问API：

```bash
# 在一个终端窗口中启动数据源服务
cd internal/sourceapp/cmd
go run main.go -db ./data/sqlite.db -http :8086
```

配置选项:
- `-db`: SQLite数据库文件路径，默认为 `./data/sqlite.db`
- `-http`: HTTP服务监听地址，默认为 `:8086`
- `-name`: 节点名称，用于日志标识

### 2. 启动缓存服务

缓存服务连接到数据源服务，并提供分布式缓存功能：

```bash
# 在另一个终端窗口中启动缓存服务
cd internal/app
go run main.go
```

## 环境变量配置

缓存服务支持通过环境变量进行配置：

- `API_PORT`: API服务器端口 (默认: "8080")
- `GROUPCACHE_PORT`: Groupcache服务器端口 (默认: "8081")
- `SELF_HOST`: 主机IP或名称 (默认: 自动检测内网IP)
- `SELF_API_ADDR`: 节点API地址 (默认: "http://<内网IP>:8080")
- `SELF_GROUPCACHE_ADDR`: 节点Groupcache地址 (默认: "http://<内网IP>:8081")
- `INITIAL_PEERS`: 初始节点列表，逗号分隔 (默认: "")
- `SOURCEAPP_SERVICE_URL`: 数据源服务URL (默认: "http://<内网IP>:8086")

示例:

```bash
# 配置并启动缓存服务
SELF_HOST=192.168.1.100 SOURCEAPP_SERVICE_URL=http://192.168.1.100:8086 API_PORT=8080 GROUPCACHE_PORT=8081 go run internal/app/main.go
```

## 内网IP自动检测

系统会自动检测您的内网IP地址，以便在局域网内正确配置服务。自动检测逻辑按以下顺序工作：

1. 查找所有网络接口，过滤掉 loopback 和 down 的接口
2. 跳过无线网卡和虚拟网卡等特殊接口
3. 查找第一个符合内网IP格式的地址(如 10.x.x.x, 172.16-31.x.x, 192.168.x.x)
4. 如果找不到合适的IP，则回退使用 "localhost"

您也可以通过 `SELF_HOST` 环境变量手动指定主机地址。

## 创建集群

启动多个缓存服务节点，并配置它们相互了解：

```bash
# 第一个节点 (192.168.1.100)
API_PORT=8080 GROUPCACHE_PORT=8081 go run internal/app/main.go

# 第二个节点 (192.168.1.101)
API_PORT=8082 GROUPCACHE_PORT=8083 INITIAL_PEERS=http://192.168.1.100:8080 go run internal/app/main.go

# 第三个节点 (192.168.1.102)
API_PORT=8084 GROUPCACHE_PORT=8085 INITIAL_PEERS=http://192.168.1.100:8080,http://192.168.1.101:8082 go run internal/app/main.go
```

## API使用

### 缓存服务API

- `GET /api/data/{key}`: 获取键值 (先尝试缓存，再尝试数据源)
- `GET /api/admin/status`: 获取节点状态
- `GET /api/admin/peers`: 获取集群节点列表

### 数据源服务API

- `GET /api/data/{key}`: 直接从数据库获取键值
- `PUT /api/data/{key}`: 存储数据
- `DELETE /api/data/{key}`: 删除数据
- `GET /api/keys`: 列出所有键
- `GET /health`: 健康检查

## 示例请求

```bash
# 通过缓存服务获取数据
curl http://192.168.1.100:8080/api/data/apple

# 直接通过数据源服务获取数据
curl http://192.168.1.100:8086/api/data/apple

# 向数据源服务添加数据
curl -X PUT -d '{"value":"something new"}' http://192.168.1.100:8086/api/data/newkey

# 查看节点状态
curl http://192.168.1.100:8080/api/admin/status
```
