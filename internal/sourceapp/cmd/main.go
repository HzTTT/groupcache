package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/golang/groupcache/internal/sourceapp"
)

func main() {
	// 解析命令行参数
	dbPath := flag.String("db", "./data/sqlite.db", "SQLite数据库文件路径")
	httpAddr := flag.String("http", ":8086", "HTTP服务监听地址")
	nodeName := flag.String("name", "sqlite-node", "节点名称")
	flag.Parse()

	// 确保数据库目录存在
	dbDir := filepath.Dir(*dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		log.Fatalf("创建数据库目录失败: %v", err)
	}

	// 创建SQLite服务
	config := sourceapp.Config{
		DbPath:   *dbPath,
		HTTPAddr: *httpAddr,
		NodeName: *nodeName,
	}

	service, err := sourceapp.NewSQLiteService(config)
	if err != nil {
		log.Fatalf("创建SQLite服务失败: %v", err)
	}

	// 处理系统信号，优雅关闭
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 启动服务（非阻塞）
	go func() {
		log.Printf("启动SQLite服务，监听地址: %s，数据库路径: %s", *httpAddr, *dbPath)
		if err := service.Start(); err != nil {
			log.Fatalf("SQLite服务启动失败: %v", err)
		}
	}()

	// 等待系统信号
	sig := <-sigChan
	log.Printf("接收到信号 %v，正在关闭服务...", sig)

	// 关闭服务
	if err := service.Stop(); err != nil {
		log.Printf("关闭服务时出错: %v", err)
	}

	log.Println("SQLite服务已安全关闭")
}
