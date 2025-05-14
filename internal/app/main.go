package main

import (
	"log"
	"time"

	"github.com/golang/groupcache/internal/app/config"
	"github.com/golang/groupcache/internal/app/datastore"
	"github.com/golang/groupcache/internal/app/gcache"
	"github.com/golang/groupcache/internal/app/peermanager"
	http_transport "github.com/golang/groupcache/internal/app/transport/http"
)

func main() {
	//log.Println("应用主函数开始执行...")

	// 1. 创建新的应用实例
	// NewApplication 内部会加载配置并初始化所有组件。
	app, err := NewApplication()
	if err != nil {
		log.Fatalf("创建应用实例失败: %v", err)
	}
	//log.Println("应用实例创建成功.")

	// 2. 启动应用
	// app.Start() 将启动所有必要的服务 (例如 PeerService, HTTP 服务器)
	// 并且通常会阻塞，直到应用被终止 (例如通过信号)。
	//log.Println("准备启动应用服务...")
	if err := app.Start(); err != nil {
		log.Fatalf("应用启动过程中发生错误: %v", err)
	}

	log.Println("应用已成功关闭.")

}

// Application 是我们应用的核心结构体，负责管理所有组件的生命周期和依赖关系。
// 它聚合了配置、数据存储、缓存服务、对等节点管理和HTTP传输层。
type Application struct {
	Config         *config.AppConfig
	Datastore      datastore.DataStore
	CachingService *gcache.CachingService
	PeerStore      *peermanager.PeerStore
	PeerService    *peermanager.PeerService
	HttpServer     *http_transport.Server
	// 用于关闭服务的清理函数
	cleanupFuncs []func() error
	// exitSignal chan os.Signal // 用于优雅关闭，当前由 HttpServer 内部处理
}

// NewApplication 创建并初始化应用的所有组件。
// 返回 Application 实例或错误（如果初始化失败）。
func NewApplication() (*Application, error) {
	//log.Println("应用初始化开始...")

	// 1. 加载配置
	appConfig := config.LoadConfig()
	log.Printf("配置已加载: API端口 %s, Groupcache端口 %s, 自身API地址: %s, 自身GC地址: %s",
		appConfig.ApiPort, appConfig.GroupcachePort, appConfig.SelfApiAddr, appConfig.SelfGroupcacheAddr)

	// 2. 初始化数据存储 (DataStore)
	var ds datastore.DataStore
	var cleanupFuncs []func() error

	// 决定使用哪种数据存储 (可以基于配置或命令行参数)
	useInMemoryStore := false // 默认使用HTTP客户端
	if useInMemoryStore {
		// 使用内存存储
		ds = datastore.NewInMemoryStore(appConfig.SelfGroupcacheAddr)
		log.Println("数据存储 (InMemoryStore) 已初始化.")
	} else {
		// 使用HTTP客户端连接sourceapp服务
		httpClientConfig := datastore.HTTPClientConfig{
			BaseURL:  appConfig.SourceappServiceURL, // 从配置中读取
			NodeName: appConfig.SelfGroupcacheAddr,
			Timeout:  5 * time.Second,
		}

		httpClient, err := datastore.NewHTTPClientProvider(httpClientConfig)
		if err != nil {
			log.Fatalf("初始化HTTP客户端失败: %v", err)
			return nil, err
		}

		ds = httpClient
		log.Printf("数据源服务地址: %s", appConfig.SourceappServiceURL)
	}

	// 3. 初始化缓存服务 (CachingService)，它内部会创建 groupcache.Group 和 groupcache.HTTPPool
	// 缓存组名和大小可以考虑也放入配置中，此处暂时硬编码。
	cachingGroupName := "distributed-cache-group" // 可以考虑从配置中读取
	cacheSizeBytes := int64(1 << 20)              // 1MB, 可以考虑从配置中读取
	cachingSvc := gcache.NewCachingService(ds, appConfig.SelfGroupcacheAddr, cachingGroupName, cacheSizeBytes)
	//log.Printf("缓存服务 (CachingService) 已初始化。组: %s, HTTPPool监听地址: %s", cachingSvc.Group.Name(), appConfig.SelfGroupcacheAddr)

	// 4. 初始化对等节点存储 (PeerStore)
	// PeerStore 需要 CachingService 中的 HTTPPool 来更新 groupcache 的对等节点列表。
	peerTimeout := 15 * time.Second // 示例值，可以从配置读取或设为常量
	ps := peermanager.NewPeerStore(
		appConfig.SelfApiAddr,
		appConfig.SelfGroupcacheAddr,
		appConfig.InitialPeerApiAddrs,
		cachingSvc.HttpPool, // 将 CachingService 的 HTTPPool 注入 PeerStore
		peerTimeout,
	)
	ps.UpdateGroupcachePoolIfNeeded() // 首次更新 groupcache 池 (此时只有自身或无对等节点)
	//log.Println("对等节点存储 (PeerStore) 已初始化.")

	// 5. 初始化对等节点管理服务 (PeerService)
	// PeerService 依赖 PeerStore，并管理宣告、心跳等后台任务。
	heartbeatInterval := 5 * time.Second // 示例值，可以从配置读取
	announceInterval := 5 * time.Second  // 示例值，可以从配置读取
	peerSvc := peermanager.NewPeerService(ps, heartbeatInterval, announceInterval)
	//log.Println("对等节点管理服务 (PeerService) 已初始化.")

	// 6. 初始化 HTTP 处理器 (Handlers)
	// Admin Handlers 依赖 PeerStore
	adminHandlers := http_transport.NewAdminHandlers(ps)
	// API Handlers 依赖 CachingService 的 Group, PeerStore, 和 AppConfig
	apiHandlers := http_transport.NewApiHandlers(cachingSvc.Group, ps, appConfig)
	//log.Println("HTTP 处理器 (AdminHandlers, ApiHandlers) 已初始化.")

	// 7. 初始化 HTTP 服务 (Server)
	// Server 依赖 AppConfig 和上面创建的 Handlers
	httpServer := http_transport.NewServer(appConfig, apiHandlers, adminHandlers)
	//log.Println("HTTP 服务 (Server) 已初始化.")

	app := &Application{
		Config:         appConfig,
		Datastore:      ds,
		CachingService: cachingSvc,
		PeerStore:      ps,
		PeerService:    peerSvc,
		HttpServer:     httpServer,
		cleanupFuncs:   cleanupFuncs,
	}

	//log.Println("应用初始化完成.")
	return app, nil
}

// Start 启动应用的所有后台服务和服务器。
// 此方法通常会阻塞，直到应用被终止。
func (a *Application) Start() error {
	//log.Printf("[%s] 应用开始启动服务...", a.Config.SelfGroupcacheAddr)

	// 1. 启动对等节点管理服务 (后台goroutines: announcer, heartbeater, pruner)
	// PeerService 的 Start 方法应该是非阻塞的（它启动goroutines）。
	a.PeerService.Start()
	//log.Printf("[%s] PeerService 已启动.", a.Config.SelfGroupcacheAddr)

	// 2. 启动 HTTP 服务器 (这将阻塞主goroutine，直到接收到关闭信号)
	// StartHttpServers 内部处理了优雅关闭的信号监听
	//log.Printf("[%s] HTTP 服务器准备启动 (API在:%s, Groupcache在:%s)...",
	//	a.Config.SelfGroupcacheAddr, a.Config.ApiPort, a.Config.GroupcachePort)
	a.HttpServer.StartHttpServers() // 此方法会阻塞直到程序通过信号退出

	// 通常， PeerService.Stop() 会在 HttpServer 接收到关闭信号后，在 main 或此处被调用。
	// 但由于 StartHttpServers() 是阻塞的并且处理了优雅关闭，我们可能需要在那里触发 PeerService 的停止，
	// 或者在 StartHttpServers() 返回后调用 PeerService.Stop()。
	// 目前，当 StartHttpServers 返回时，意味着程序即将结束。
	log.Printf("[%s] HTTP 服务已停止或即将停止。调用 PeerService.Stop()...", a.Config.SelfGroupcacheAddr)
	a.PeerService.Stop() // 确保 PeerService 的 goroutines 也被清理

	// 执行所有清理函数
	for _, cleanup := range a.cleanupFuncs {
		if err := cleanup(); err != nil {
			log.Printf("[%s] 执行清理函数时发生错误: %v", a.Config.SelfGroupcacheAddr, err)
		}
	}

	log.Printf("[%s] 应用服务已全部停止.", a.Config.SelfGroupcacheAddr)
	return nil
}

// Stop 显式停止应用服务。
// 对于需要从外部控制停止的情况（例如，测试或更复杂的生命周期管理）。
func (a *Application) Stop() {
	log.Printf("[%s] 应用明确调用 Stop()...", a.Config.SelfGroupcacheAddr)
	// 优雅地停止 PeerService (它会等待其goroutines完成)
	if a.PeerService != nil {
		a.PeerService.Stop()
	}

	// 执行所有清理函数
	for _, cleanup := range a.cleanupFuncs {
		if err := cleanup(); err != nil {
			log.Printf("[%s] 执行清理函数时发生错误: %v", a.Config.SelfGroupcacheAddr, err)
		}
	}

	// HTTP Server 的关闭由其自身的 StartHttpServers 方法中的信号处理逻辑控制，
	// 或者如果 StartHttpServers 设计为非阻塞的，这里可以调用其特定的 Shutdown 方法。
	// 假设 StartHttpServers 是阻塞的，并且在返回时已经完成了关闭。
	log.Printf("[%s] 应用 Stop() 完成.", a.Config.SelfGroupcacheAddr)
}
