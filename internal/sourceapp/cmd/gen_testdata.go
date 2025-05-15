package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/golang/groupcache/internal/sourceapp"
)

// TestItem 表示测试数据项
type TestItem struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Tags        []string  `json:"tags"`
	Score       float64   `json:"score"`
	IsActive    bool      `json:"is_active"`
	CreatedAt   time.Time `json:"created_at"`
}

// 生成随机字符串
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// 生成随机标签列表
func randomTags() []string {
	tags := []string{"技术", "教育", "娱乐", "新闻", "体育", "游戏", "音乐", "电影", "科学", "艺术", "历史", "健康"}
	numTags := rand.Intn(5) + 1
	selectedTags := make([]string, 0, numTags)

	for i := 0; i < numTags; i++ {
		selectedTags = append(selectedTags, tags[rand.Intn(len(tags))])
	}

	return selectedTags
}

// 生成测试数据项
func generateTestItem(id int) TestItem {
	return TestItem{
		ID:          id,
		Name:        fmt.Sprintf("项目-%s", randomString(8)),
		Description: fmt.Sprintf("这是一个随机生成的测试项目描述，ID为%d，包含一些随机内容: %s", id, randomString(50)),
		Tags:        randomTags(),
		Score:       float64(rand.Intn(100)) / 10.0,
		IsActive:    rand.Intn(2) == 1,
		CreatedAt:   time.Now().Add(-time.Duration(rand.Intn(30*24)) * time.Hour), // 随机日期，最多30天前
	}
}

// 生成不同类型的键
func generateKey(prefix string, id int) string {
	return fmt.Sprintf("%s_data_%d", prefix, id)
}

func main() {
	// 解析命令行参数
	dbPath := flag.String("db", "./data/sqlite.db", "SQLite数据库文件路径")
	count := flag.Int("count", 100, "要生成的测试数据项数量")
	prefix := flag.String("prefix", "test", "键名前缀")
	clear := flag.Bool("clear", false, "是否先清除现有数据")
	flag.Parse()

	// 确保数据库目录存在
	dbDir := filepath.Dir(*dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		log.Fatalf("创建数据库目录失败: %v", err)
	}

	// 设置随机种子
	rand.Seed(time.Now().UnixNano())

	// 创建SQLite服务
	config := sourceapp.Config{
		DbPath:   *dbPath,
		HTTPAddr: ":0", // 不需要启动HTTP服务
		NodeName: "test-data-generator",
	}

	service, err := sourceapp.NewSQLiteService(config)
	if err != nil {
		log.Fatalf("创建SQLite服务失败: %v", err)
	}
	defer service.Stop()

	// 如果需要清除现有数据
	if *clear {
		fmt.Println("正在清除以前缀开头的现有数据...")
		clearExistingData(service, *prefix)
	}

	// 生成并插入测试数据
	fmt.Printf("正在生成 %d 条测试数据...\n", *count)

	for i := 1; i <= *count; i++ {
		// 生成测试项
		item := generateTestItem(i)

		// 将测试项转换为JSON
		data, err := json.Marshal(item)
		if err != nil {
			log.Printf("无法序列化测试项 %d: %v", i, err)
			continue
		}

		// 生成键名
		key := generateKey(*prefix, i)

		// 存储数据
		if err := service.Set(key, data); err != nil {
			log.Printf("存储测试项 %s 失败: %v", key, err)
			continue
		}

		if i%10 == 0 || i == *count {
			fmt.Printf("已生成 %d/%d 条测试数据\n", i, *count)
		}
	}

	fmt.Println("测试数据生成完成！")
}

// 清除指定前缀的现有数据
func clearExistingData(service *sourceapp.SQLiteService, prefix string) {
	// 这里需要访问底层数据库以执行前缀删除
	// 由于SQLiteService不直接提供此功能，可以通过直接访问db字段或修改service接口
	// 这里简单地输出一条消息，在实际应用中需要实现
	fmt.Println("注意: 清除功能需要实现，此处是占位符。")
}
