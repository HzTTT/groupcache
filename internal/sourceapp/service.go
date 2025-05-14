package sourceapp

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// SQLiteService 提供基于SQLite的数据服务
type SQLiteService struct {
	// db 是SQLite数据库连接
	db *sql.DB
	// dbPath 是数据库文件路径
	dbPath string
	// httpAddr 是服务监听的地址，例如 ":8080"
	httpAddr string
	// nodeName 用于标识该服务实例
	nodeName string
}

// Config SQLite服务配置
type Config struct {
	// DbPath 是SQLite数据库文件的路径
	DbPath string
	// HTTPAddr 服务监听的地址，例如 ":8080"
	HTTPAddr string
	// NodeName 用于标识该服务实例
	NodeName string
}

// NewSQLiteService 创建一个新的SQLite服务
func NewSQLiteService(config Config) (*SQLiteService, error) {
	// 确保数据库目录存在
	dbDir := filepath.Dir(config.DbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}

	// 打开SQLite数据库连接
	db, err := sql.Open("sqlite3", config.DbPath+"?_journal=WAL&_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("打开数据库连接失败: %w", err)
	}

	// 测试连接
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连接测试失败: %w", err)
	}

	// 创建服务实例
	service := &SQLiteService{
		db:       db,
		dbPath:   config.DbPath,
		httpAddr: config.HTTPAddr,
		nodeName: config.NodeName,
	}

	// 初始化数据库表
	if err := service.initDatabase(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化数据库失败: %w", err)
	}

	return service, nil
}

// initDatabase 初始化数据库表结构
func (s *SQLiteService) initDatabase() error {
	// 创建数据表
	_, err := s.db.Exec(`
	CREATE TABLE IF NOT EXISTS items (
		key TEXT PRIMARY KEY,
		value BLOB,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		return fmt.Errorf("创建items表失败: %w", err)
	}

	// 创建更新时间触发器
	_, err = s.db.Exec(`
	CREATE TRIGGER IF NOT EXISTS update_items_timestamp
	AFTER UPDATE ON items
	BEGIN
		UPDATE items SET updated_at = CURRENT_TIMESTAMP WHERE key = NEW.key;
	END;
	`)
	if err != nil {
		return fmt.Errorf("创建触发器失败: %w", err)
	}

	return nil
}

// Start 启动SQLite服务
func (s *SQLiteService) Start() error {
	// 设置HTTP路由
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data/", s.handleData)
	mux.HandleFunc("/api/keys", s.handleListKeys)
	mux.HandleFunc("/health", s.handleHealth)

	// 启动HTTP服务器
	log.Printf("[SQLite服务] 节点 %s: 在 %s 上启动HTTP服务", s.nodeName, s.httpAddr)
	return http.ListenAndServe(s.httpAddr, mux)
}

// Stop 停止SQLite服务
func (s *SQLiteService) Stop() error {
	log.Printf("[SQLite服务] 节点 %s: 关闭服务", s.nodeName)
	return s.db.Close()
}

// handleData 处理数据的增删改查
func (s *SQLiteService) handleData(w http.ResponseWriter, r *http.Request) {
	// 提取键名
	key := r.URL.Path[len("/api/data/"):]
	if key == "" {
		http.Error(w, "键名不能为空", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		// 读取数据
		var value []byte
		var createdAt, updatedAt string
		err := s.db.QueryRow("SELECT value, created_at, updated_at FROM items WHERE key = ?", key).Scan(&value, &createdAt, &updatedAt)
		if err == sql.ErrNoRows {
			http.Error(w, "找不到指定的键", http.StatusNotFound)
			return
		}
		if err != nil {
			log.Printf("[SQLite服务] 节点 %s: 读取键 %s 失败: %v", s.nodeName, key, err)
			http.Error(w, "读取数据失败", http.StatusInternalServerError)
			return
		}

		// 设置响应头
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Created-At", createdAt)
		w.Header().Set("X-Updated-At", updatedAt)
		w.Write(value)

	case http.MethodPut:
		// 读取请求体
		var value []byte
		var err error
		value = make([]byte, r.ContentLength)
		_, err = r.Body.Read(value)
		if err != nil && err.Error() != "EOF" {
			log.Printf("[SQLite服务] 节点 %s: 读取请求体失败: %v", s.nodeName, err)
			http.Error(w, "读取请求体失败", http.StatusBadRequest)
			return
		}

		// 存储数据
		_, err = s.db.Exec("INSERT OR REPLACE INTO items(key, value) VALUES(?, ?)", key, value)
		if err != nil {
			log.Printf("[SQLite服务] 节点 %s: 存储键 %s 失败: %v", s.nodeName, key, err)
			http.Error(w, "存储数据失败", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","message":"数据已成功存储"}`))

	case http.MethodDelete:
		// 删除数据
		result, err := s.db.Exec("DELETE FROM items WHERE key = ?", key)
		if err != nil {
			log.Printf("[SQLite服务] 节点 %s: 删除键 %s 失败: %v", s.nodeName, key, err)
			http.Error(w, "删除数据失败", http.StatusInternalServerError)
			return
		}

		affected, _ := result.RowsAffected()
		if affected == 0 {
			http.Error(w, "找不到指定的键", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","message":"数据已成功删除"}`))

	default:
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
	}
}

// handleListKeys 列出所有键
func (s *SQLiteService) handleListKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "只支持GET方法", http.StatusMethodNotAllowed)
		return
	}

	// 解析查询参数
	limit := 100 // 默认限制
	offset := 0  // 默认偏移
	prefix := "" // 可选前缀过滤

	if r.URL.Query().Get("limit") != "" {
		if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
			limit = l
		}
	}

	if r.URL.Query().Get("offset") != "" {
		if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
			offset = o
		}
	}

	prefix = r.URL.Query().Get("prefix")

	// 查询键
	var rows *sql.Rows
	var err error
	if prefix != "" {
		rows, err = s.db.Query("SELECT key, created_at, updated_at FROM items WHERE key LIKE ? ORDER BY key LIMIT ? OFFSET ?",
			prefix+"%", limit, offset)
	} else {
		rows, err = s.db.Query("SELECT key, created_at, updated_at FROM items ORDER BY key LIMIT ? OFFSET ?",
			limit, offset)
	}

	if err != nil {
		log.Printf("[SQLite服务] 节点 %s: 查询键列表失败: %v", s.nodeName, err)
		http.Error(w, "查询键列表失败", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// 构建响应
	type KeyInfo struct {
		Key       string `json:"key"`
		CreatedAt string `json:"created_at"`
		UpdatedAt string `json:"updated_at"`
	}

	keys := []KeyInfo{}
	for rows.Next() {
		var key, createdAt, updatedAt string
		if err := rows.Scan(&key, &createdAt, &updatedAt); err != nil {
			log.Printf("[SQLite服务] 节点 %s: 扫描键数据失败: %v", s.nodeName, err)
			continue
		}
		keys = append(keys, KeyInfo{
			Key:       key,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		})
	}

	// 获取总数
	var total int
	if prefix != "" {
		err = s.db.QueryRow("SELECT COUNT(*) FROM items WHERE key LIKE ?", prefix+"%").Scan(&total)
	} else {
		err = s.db.QueryRow("SELECT COUNT(*) FROM items").Scan(&total)
	}

	if err != nil {
		log.Printf("[SQLite服务] 节点 %s: 获取键总数失败: %v", s.nodeName, err)
		total = -1
	}

	response := struct {
		Total  int       `json:"total"`
		Limit  int       `json:"limit"`
		Offset int       `json:"offset"`
		Keys   []KeyInfo `json:"keys"`
	}{
		Total:  total,
		Limit:  limit,
		Offset: offset,
		Keys:   keys,
	}

	// 返回JSON
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("[SQLite服务] 节点 %s: 编码JSON响应失败: %v", s.nodeName, err)
		http.Error(w, "服务器内部错误", http.StatusInternalServerError)
	}
}

// handleHealth 健康检查
func (s *SQLiteService) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "只支持GET方法", http.StatusMethodNotAllowed)
		return
	}

	// 检查数据库连接
	if err := s.db.Ping(); err != nil {
		log.Printf("[SQLite服务] 节点 %s: 健康检查失败: %v", s.nodeName, err)
		http.Error(w, "数据库连接失败", http.StatusServiceUnavailable)
		return
	}

	response := struct {
		Status    string `json:"status"`
		Timestamp string `json:"timestamp"`
		Node      string `json:"node"`
		DbPath    string `json:"db_path"`
	}{
		Status:    "healthy",
		Timestamp: time.Now().Format(time.RFC3339),
		Node:      s.nodeName,
		DbPath:    s.dbPath,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("[SQLite服务] 节点 %s: 编码健康检查响应失败: %v", s.nodeName, err)
		http.Error(w, "服务器内部错误", http.StatusInternalServerError)
	}
}

// Get 从数据库中获取值
func (s *SQLiteService) Get(key string) ([]byte, error) {
	var value []byte
	err := s.db.QueryRow("SELECT value FROM items WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("键 %s 不存在", key)
	}
	if err != nil {
		return nil, fmt.Errorf("获取键 %s 失败: %w", key, err)
	}
	return value, nil
}

// Set 将值存入数据库
func (s *SQLiteService) Set(key string, value []byte) error {
	_, err := s.db.Exec("INSERT OR REPLACE INTO items(key, value) VALUES(?, ?)", key, value)
	if err != nil {
		return fmt.Errorf("存储键 %s 失败: %w", key, err)
	}
	return nil
}

// Delete 从数据库中删除键
func (s *SQLiteService) Delete(key string) error {
	result, err := s.db.Exec("DELETE FROM items WHERE key = ?", key)
	if err != nil {
		return fmt.Errorf("删除键 %s 失败: %w", key, err)
	}
	affected, _ := result.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("键 %s 不存在", key)
	}
	return nil
}
