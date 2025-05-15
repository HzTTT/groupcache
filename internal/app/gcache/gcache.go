package gcache

import (
	"context"
	"fmt"

	"github.com/golang/groupcache"
	"github.com/golang/groupcache/internal/app/datastore"
)

const (
	DefaultGroupName      = "my-default-data-group"
	DefaultCacheSizeBytes = 1 << 20 // 1MB
)

// CachingService 封装了 groupcache 的设置和获取函数。
// 它持有 groupcache Group、HTTPPool 和底层数据存储的引用。
type CachingService struct {
	Group          *groupcache.Group
	HttpPool       *groupcache.HTTPPool
	dataStore      datastore.DataStore // 使用接口而不是具体实现
	nodeAddress    string              // 用于日志记录，通常是配置中的 SelfGroupcacheAddr
	groupName      string
	cacheSizeBytes int64
}

// NewCachingService 创建并初始化 groupcache Group 和 HTTPPool。
func NewCachingService(
	dataStore datastore.DataStore, // 修改为接受接口
	selfGroupcacheAddr string, // 例如，http://localhost:8081，用于 nodeAddress 日志记录和 HTTPPool 自身 ID
	groupName string,
	cacheSizeBytes int64,
) *CachingService {
	if groupName == "" {
		groupName = DefaultGroupName
	}
	if cacheSizeBytes == 0 {
		cacheSizeBytes = DefaultCacheSizeBytes
	}

	cs := &CachingService{
		dataStore:      dataStore,
		nodeAddress:    selfGroupcacheAddr,
		groupName:      groupName,
		cacheSizeBytes: cacheSizeBytes,
	}

	//log.Printf("[%s CachingService] 正在初始化 groupcache 组 '%s'，缓存大小 %d 字节", cs.nodeAddress, cs.groupName, cs.cacheSizeBytes)
	// getterFunc 现在是 CachingService 的一个方法，因此它可以访问 cs.dataStore 和 cs.nodeAddress。
	cs.Group = groupcache.NewGroup(cs.groupName, cs.cacheSizeBytes, groupcache.GetterFunc(cs.getterFunc))

	//log.Printf("[%s CachingService] 正在初始化 HTTPPool，自身地址: %s", cs.nodeAddress, cs.nodeAddress)
	cs.HttpPool = groupcache.NewHTTPPool(cs.nodeAddress) // NewHTTPPool 在 http.DefaultServeMux 的 /_groupcache/ 路径注册了一个 HTTP 处理程序

	return cs
}

// getterFunc 定义了如何在缓存或对等节点中不存在数据时加载数据。
// 它是 CachingService 的一个方法，以访问其依赖项，如 dataStore。
func (cs *CachingService) getterFunc(ctx context.Context, key string, dest groupcache.Sink) error {
	//log.Printf("[获取器] 节点 %s，组 %s：被调用获取键: %q。", cs.nodeAddress, cs.groupName, key)

	val, err := cs.dataStore.Get(key) // 使用注入的数据存储
	if err != nil {
		//log.Printf("[获取器] 节点 %s，组 %s：数据存储中未找到键 %q: %v", cs.nodeAddress, cs.groupName, key, err)
		// 这里返回的错误必须是 groupcache 能够理解的错误，
		// 或者至少是一个表示未找到键的错误，这样它就不会被缓存为该键的永久错误。
		// groupcache 本身没有针对此的特定错误类型，fmt.Errorf 已足够。
		return fmt.Errorf("通过缓存服务在数据存储中未找到键: %s: %w", key, err)
	}

	// datastore.Get 方法已经返回了一个副本，所以这里不需要再复制一次。
	if err := dest.SetBytes(val); err != nil {
		//log.Printf("[获取器] 节点 %s，组 %s：为键 %q 设置字节时出错: %v", cs.nodeAddress, cs.groupName, key, err)
		return err
	}
	//log.Printf("[获取器] 节点 %s，组 %s：成功为键 %q 在缓存接收器中设置字节", cs.nodeAddress, cs.groupName, key)
	return nil
}
