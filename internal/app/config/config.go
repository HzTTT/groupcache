package config

import (
	"log"
	"net"
	"os"
	"strings"
)

// AppConfig 保存应用程序的配置
type AppConfig struct {
	// ApiPort 是 HTTP API 服务器监听的端口
	ApiPort string
	// GroupcachePort 是 Groupcache HTTP 服务器监听的端口
	GroupcachePort string
	// SelfApiAddr 是此节点的完整 API 地址，例如 http://localhost:8080
	SelfApiAddr string
	// SelfGroupcacheAddr 是此节点的完整 Groupcache 地址，例如 http://localhost:8081
	SelfGroupcacheAddr string
	// InitialPeerApiAddrs 是初始对等节点的 API 地址列表
	InitialPeerApiAddrs []string
	// SourceappServiceURL 是 sourceapp 服务的URL，例如 http://localhost:8086
	SourceappServiceURL string
}

// 获取默认内网IP
func getLocalIP() string {
	// 默认的fallback IP
	defaultIP := "localhost"

	// 获取所有网络接口
	ifaces, err := net.Interfaces()
	if err != nil {
		log.Printf("获取网络接口失败: %v, 使用默认值: %s", err, defaultIP)
		return defaultIP
	}

	// 遍历所有网络接口
	for _, iface := range ifaces {
		// 忽略down的接口和loopback接口
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// 忽略无线网卡、虚拟网卡等特殊接口
		if strings.Contains(iface.Name, "wl") || strings.Contains(iface.Name, "vmnet") ||
			strings.Contains(iface.Name, "veth") || strings.Contains(iface.Name, "docker") {
			continue
		}

		// 获取接口的地址
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		// 遍历地址，找到第一个非环回的内网IPv4地址
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			// 确保是IPv4地址
			if ip == nil || ip.To4() == nil {
				continue
			}

			// 检查是否是内网地址
			if !ip.IsLoopback() && (ip.IsPrivate() || isLocalSubnet(ip)) {
				log.Printf("找到内网IP: %s (接口: %s)", ip.String(), iface.Name)
				return ip.String()
			}
		}
	}

	log.Printf("未找到内网IP，使用默认值: %s", defaultIP)
	return defaultIP
}

// 检查IP是否属于常见的内网子网
func isLocalSubnet(ip net.IP) bool {
	// 使用CIDR判断是否为私有IP段
	// 10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16
	privateRanges := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"}

	for _, cidr := range privateRanges {
		_, subnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}

// LoadConfig 从环境变量加载配置，并应用默认值
func LoadConfig() *AppConfig {
	apiPort := getEnvOrDefault("API_PORT", "8080")
	gcPort := getEnvOrDefault("GROUPCACHE_PORT", "8081")

	// 从环境变量获取主机名或内网IP
	selfHost := getEnvOrDefault("SELF_HOST", getLocalIP())
	log.Printf("使用主机地址: %s", selfHost)

	selfApiAddr := getEnvOrDefault("SELF_API_ADDR", "http://"+selfHost+":"+apiPort)
	selfGCAddr := getEnvOrDefault("SELF_GROUPCACHE_ADDR", "http://"+selfHost+":"+gcPort)

	peersStr := getEnvOrDefault("INITIAL_PEERS", "")
	var peers []string
	if peersStr != "" {
		peers = strings.Split(peersStr, ",")
	}

	sourceappURL := getEnvOrDefault("SOURCEAPP_SERVICE_URL", "http://"+selfHost+":8086")

	return &AppConfig{
		ApiPort:             apiPort,
		GroupcachePort:      gcPort,
		SelfApiAddr:         selfApiAddr,
		SelfGroupcacheAddr:  selfGCAddr,
		InitialPeerApiAddrs: peers,
		SourceappServiceURL: sourceappURL,
	}
}

// getEnvOrDefault 从环境变量获取值，如果不存在则返回默认值
func getEnvOrDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
