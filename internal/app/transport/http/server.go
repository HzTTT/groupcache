package http

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/golang/groupcache/internal/app/config"
)

// Server 代表了应用程序的组合 HTTP 服务器功能。
// 它可以管理多个监听器（例如，一个用于 API，一个用于 groupcache 对等节点）。
// 注意：处理程序（例如，"handlers"）的包名如果它们在同一个包中，则隐式为 "http"。
// 如果处理程序位于例如 "handlers_api" 子包中，则它们将被限定。
type Server struct {
	appConfig *config.AppConfig
	apiMux    *http.ServeMux // 用于客户端 API 和管理端点

	ApiHandlers   *ApiHandlers   // 来自 handlers_api.go (在同一个包 'http' 中)
	AdminHandlers *AdminHandlers // 来自 handlers_admin.go (在同一个包 'http' 中)
}

// NewServer 创建一个新的 Server 实例。
// 它初始化 apiMux，但尚未从 ApiHandlers/AdminHandlers 注册特定的处理程序。
// 处理程序注册应在 ApiHandlers/AdminHandlers 本身及其依赖项创建之后完成。
func NewServer(appConfig *config.AppConfig, apiHandlers *ApiHandlers, adminHandlers *AdminHandlers) *Server {
	srv := &Server{
		appConfig:     appConfig,
		apiMux:        http.NewServeMux(),
		ApiHandlers:   apiHandlers,
		AdminHandlers: adminHandlers,
	}
	srv.registerRoutes()
	return srv
}

// registerRoutes 设置 API 和管理处理程序的 HTTP 路由。
// 此函数由 NewServer 内部调用。
func (s *Server) registerRoutes() {
	if s.ApiHandlers == nil || s.AdminHandlers == nil {
		// 如果 appConfig 为 nil，则 SelfApiAddr 可能未初始化，处理该潜在的 panic。
		logMsgPrefix := "[未知节点 HTTP 服务器]"
		if s.appConfig != nil {
			logMsgPrefix = fmt.Sprintf("[%s HTTP 服务器]", s.appConfig.SelfApiAddr)
		}
		log.Printf("%s 警告: 未提供 ApiHandlers 或 AdminHandlers，某些路由将不会被注册。", logMsgPrefix)
		return
	}

	// API 路由
	s.apiMux.HandleFunc("/get", s.ApiHandlers.GetHandler)
	s.apiMux.HandleFunc("/ping_api", s.ApiHandlers.PingApiHandler)
	s.apiMux.HandleFunc("/admin/known_peers", s.ApiHandlers.KnownPeersHandler) // 调试/信息端点

	// 用于对等节点管理的管理路由
	s.apiMux.HandleFunc("/admin/announce_self", s.AdminHandlers.AnnounceSelfHandler)
	s.apiMux.HandleFunc("/admin/heartbeat", s.AdminHandlers.HeartbeatHandler)
	//log.Printf("[%s HTTP 服务器] API 和管理路由已注册。", s.appConfig.SelfApiAddr)
}

// StartHttpServers 启动 API/Admin 服务器和 groupcache 对等通信服务器。
// 它会阻塞直到收到关闭信号以进行优雅终止。
func (s *Server) StartHttpServers() {
	// 用于监听服务器 goroutine 错误的通道
	errChan := make(chan error, 2)

	// 启动 groupcache 对等通信服务器 (监听 appConfig.GroupcachePort)
	// 这使用 http.DefaultServeMux，groupcache.HTTPPool (来自 gcache 模块) 在此注册自身。
	peerHttpServer := &http.Server{
		Addr:    ":" + s.appConfig.GroupcachePort,
		Handler: http.DefaultServeMux, // groupcache HTTPPool 应该已经在此注册
	}
	go func() {
		//log.Printf("Groupcache 对等服务器正在启动，监听端口: %s (用于 /_groupcache/ 路径)", s.appConfig.GroupcachePort)
		if err := peerHttpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("启动 groupcache 对等服务器时出错: %v", err)
			errChan <- fmt.Errorf("groupcache 对等服务器失败: %w", err)
		}
		//log.Printf("Groupcache 对等服务器 (端口 %s) 已关闭。", s.appConfig.GroupcachePort)
	}()

	// 启动 API 服务器 (监听 appConfig.ApiPort)
	apiHttpServer := &http.Server{
		Addr:    ":" + s.appConfig.ApiPort,
		Handler: s.apiMux, // 使用已注册 API 和管理处理程序的 mux
	}
	go func() {
		//log.Printf("API 服务器 (客户端请求和管理) 正在启动，监听端口: %s", s.appConfig.ApiPort)
		if err := apiHttpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("启动 API 服务器时出错: %v", err)
			errChan <- fmt.Errorf("API 服务器失败: %w", err)
		}
		log.Printf("API 服务器 (端口 %s) 已关闭。", s.appConfig.ApiPort)
	}()

	// 等待中断信号或服务器错误
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errChan:
		log.Fatalf("严重的服务器错误: %v。正在关闭。", err)
	case sig := <-quit:
		log.Printf("收到关闭信号 %v，正在优雅地关闭服务器...", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // 优雅关闭的10秒超时
	defer cancel()

	// 关闭 API 服务器
	log.Println("尝试关闭 API 服务器...")
	if err := apiHttpServer.Shutdown(ctx); err != nil {
		log.Printf("API 服务器被强制关闭: %v", err)
	} else {
		log.Println("API 服务器已优雅关闭。")
	}

	// 关闭 groupcache 对等服务器
	log.Println("尝试关闭 groupcache 对等服务器...")
	if err := peerHttpServer.Shutdown(ctx); err != nil {
		log.Printf("Groupcache 对等服务器被强制关闭: %v", err)
	} else {
		log.Println("Groupcache 对等服务器已优雅关闭。")
	}

	log.Println("所有 HTTP 服务器关闭过程已完成。")
}
