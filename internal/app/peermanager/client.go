package peermanager

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	// Since protocol.go is in the same package peermanager, AnnouncePayload etc. are directly available.
)

const (
	DefaultHttpClientTimeout = 3 * time.Second
)

// sendPostRequest 是一个辅助函数，用于向目标 URL 发送 JSON POST 请求。
// 如果请求成功且 responseData 不为 nil，则会填充 responseData。
func sendPostRequest(targetUrl string, payload interface{}, responseData interface{}, timeout time.Duration) error {
	if timeout == 0 {
		timeout = DefaultHttpClientTimeout
	}
	client := http.Client{
		Timeout: timeout,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("为 %s 序列化载荷失败: %w", targetUrl, err)
	}

	req, err := http.NewRequest("POST", targetUrl, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("为 %s 创建请求失败: %w", targetUrl, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("向 %s 发送请求失败: %w", targetUrl, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("向 %s 的请求失败，状态: %s", targetUrl, resp.Status)
	}

	if responseData != nil {
		if err := json.NewDecoder(resp.Body).Decode(responseData); err != nil {
			return fmt.Errorf("从 %s 解码响应失败: %w", targetUrl, err)
		}
	}
	return nil
}
