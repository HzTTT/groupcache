# SQLite 数据服务

这是一个基于SQLite的数据服务，提供简单的键值存储功能，同时支持HTTP API接口。

## 功能特点

- 使用SQLite作为底层存储引擎，提供持久化的键值存储
- 支持通过HTTP API进行数据的增删改查
- 提供健康检查API
- 支持按前缀过滤和分页查询键列表
- 记录数据的创建和更新时间
- 优雅的服务启动和关闭处理
- 可与groupcache集成，作为数据源使用

## 与groupcache集成

SQLite数据服务可以作为groupcache的数据源使用，取代内存存储。我们提供了一个适配器，使其符合groupcache需要的接口：

```go
// 在应用中使用SQLite数据服务作为groupcache的数据源
import (
    "github.com/golang/groupcache/internal/app/datasource"
    "github.com/golang/groupcache/internal/sourceapp"
)

// 配置SQLite服务
sqliteConfig := sourceapp.Config{
    DbPath:   "./data/sqlite.db",
    HTTPAddr: ":8086",
    NodeName: "node-1",
}

// 创建数据提供者
providerConfig := datasource.DataProviderConfig{
    SQLiteConfig: sqliteConfig,
    LoadTestData: true, // 自动加载测试数据
}

// 初始化SQLite数据提供者
sqliteProvider, err := datasource.NewSQLiteDataProviderWithConfig(providerConfig)
if err != nil {
    log.Fatalf("初始化SQLite数据源失败: %v", err)
}

// 将SQLite数据提供者作为数据源传递给CachingService
cachingService := gcache.NewCachingService(
    sqliteProvider,
    selfGroupcacheAddr,
    groupName,
    cacheSizeBytes,
)

// 在应用停止时关闭SQLite服务
defer sqliteProvider.Close()
```

## API接口

### 数据操作

#### 获取数据

- **URL**: `/api/data/{key}`
- **方法**: `GET`
- **说明**: 获取指定键的数据
- **返回**: 
  - 成功: 200 OK，返回JSON数据
  - 失败: 404 Not Found，键不存在

#### 存储数据

- **URL**: `/api/data/{key}`
- **方法**: `PUT`
- **Body**: JSON格式数据
- **说明**: 存储数据到指定键
- **返回**: 
  - 成功: 200 OK，返回`{"status":"success","message":"数据已成功存储"}`
  - 失败: 400 Bad Request 或 500 Internal Server Error

#### 删除数据

- **URL**: `/api/data/{key}`
- **方法**: `DELETE`
- **说明**: 删除指定键的数据
- **返回**: 
  - 成功: 200 OK，返回`{"status":"success","message":"数据已成功删除"}`
  - 失败: 404 Not Found 或 500 Internal Server Error

### 元数据操作

#### 列出键

- **URL**: `/api/keys`
- **方法**: `GET`
- **参数**:
  - `limit`: 每页返回的键数量，默认100
  - `offset`: 偏移量，默认0
  - `prefix`: 键前缀过滤
- **说明**: 列出所有键，支持分页和前缀过滤
- **返回**: 
  - 成功: 200 OK，返回JSON数据，包含键列表、总数、分页信息
  - 失败: 500 Internal Server Error

### 健康检查

- **URL**: `/health`
- **方法**: `GET`
- **说明**: 检查服务是否正常运行
- **返回**: 
  - 成功: 200 OK，返回`{"status":"healthy","timestamp":"...","node":"...","db_path":"..."}`
  - 失败: 503 Service Unavailable

## 命令行参数

启动服务时可以指定以下命令行参数：

- `-db`: SQLite数据库文件路径，默认为 `./data/sqlite.db`
- `-http`: HTTP服务监听地址，默认为 `:8086`
- `-name`: 节点名称，默认为 `sqlite-node`

## 使用示例

### 启动服务

```bash
# 使用默认配置启动
./sqliteservice

# 指定数据库路径和端口
./sqliteservice -db /path/to/data.db -http :9000
```

### API使用示例

```bash
# 存储数据
curl -X PUT -d '{"name":"example","value":42}' http://localhost:8086/api/data/mykey

# 获取数据
curl http://localhost:8086/api/data/mykey

# 列出所有键
curl http://localhost:8086/api/keys

# 按前缀过滤键
curl http://localhost:8086/api/keys?prefix=my&limit=10

# 删除数据
curl -X DELETE http://localhost:8086/api/data/mykey

# 健康检查
curl http://localhost:8086/health
```

## 作为库使用

可以在Go代码中将此服务作为库引用：

```go
import "github.com/golang/groupcache/internal/sourceapp"

config := sourceapp.Config{
    DbPath:   "./data.db",
    HTTPAddr: ":8086",
    NodeName: "node1",
}

service, err := sourceapp.NewSQLiteService(config)
if err != nil {
    log.Fatalf("创建SQLite服务失败: %v", err)
}

// 启动服务
go service.Start()

// 使用服务API
err = service.Set("mykey", []byte(`{"name":"example"}`))
data, err := service.Get("mykey")

// 关闭服务
service.Stop()
``` 