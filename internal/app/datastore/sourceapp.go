package datastore

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// HTTPClientProvider 是一个通过HTTP API调用sourceapp服务的适配器
type HTTPClientProvider struct {
	// 服务器基础URL
	baseURL string
	// 节点名称（用于日志）
	nodeName string
	// HTTP客户端
	client *http.Client
}

// HTTPClientConfig 配置HTTP客户端
type HTTPClientConfig struct {
	// BaseURL 是sourceapp服务的基础URL，例如 "http://localhost:8086"
	BaseURL string
	// NodeName 用于日志标识
	NodeName string
	// Timeout HTTP请求超时时间
	Timeout time.Duration
}

// NewHTTPClientProvider 创建一个新的HTTP客户端适配器
func NewHTTPClientProvider(config HTTPClientConfig) (*HTTPClientProvider, error) {
	if config.BaseURL == "" {
		return nil, fmt.Errorf("必须提供baseURL")
	}

	// 设置默认超时
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 5 * time.Second
	}

	// 创建HTTP客户端
	client := &http.Client{
		Timeout: timeout,
	}

	return &HTTPClientProvider{
		baseURL:  config.BaseURL,
		nodeName: config.NodeName,
		client:   client,
	}, nil
}

// Get 通过HTTP API获取数据
func (p *HTTPClientProvider) Get(key string) ([]byte, error) {
	log.Printf("[HTTP客户端] 节点 %s: 通过API获取键: %q", p.nodeName, key)

	// 构建URL
	url := fmt.Sprintf("%s/api/data/%s", p.baseURL, key)

	// 发送GET请求
	resp, err := p.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode == http.StatusNotFound {
		log.Printf("[HTTP客户端] 节点 %s: 服务器未找到键 %q", p.nodeName, key)
		return nil, fmt.Errorf("键不存在: %s", key)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("服务器返回状态码: %d", resp.StatusCode)
	}

	// 读取响应体
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	return data, nil
}
