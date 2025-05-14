package main

import (
	"log"
)

func main() {
	log.Println("应用主函数开始执行...")

	// 1. 创建新的应用实例
	// NewApplication 内部会加载配置并初始化所有组件。
	app, err := NewApplication()
	if err != nil {
		log.Fatalf("创建应用实例失败: %v", err)
	}
	log.Println("应用实例创建成功.")

	// 2. 启动应用
	// app.Start() 将启动所有必要的服务 (例如 PeerService, HTTP 服务器)
	// 并且通常会阻塞，直到应用被终止 (例如通过信号)。
	log.Println("准备启动应用服务...")
	if err := app.Start(); err != nil {
		log.Fatalf("应用启动过程中发生错误: %v", err)
	}

	log.Println("应用已成功关闭.")
}
