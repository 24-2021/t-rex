package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// saveToDisk 将会话保存到磁盘
func saveToDisk(storageDir string, sess *Session) error {
	filePath := filepath.Join(storageDir, sess.ID+".json")

	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filePath, data, 0644)
}

// loadFromDisk 从磁盘加载会话
func (m *Manager) loadFromDisk(id string) (*Session, error) {
	filePath := filepath.Join(m.storageDir, id+".json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	// 缓存
	m.sessions[session.ID] = &session

	return &session, nil
}

// ListAllSessions 列出所有会话（用于 CLI 显示）
func ListAllSessions(storageDir string) ([]SessionSummary, error) {
	entries, err := os.ReadDir(storageDir)
	if err != nil {
		// 目录不存在，返回空列表
		if os.IsNotExist(err) {
			return []SessionSummary{}, nil
		}
		return nil, err
	}

	var summaries []SessionSummary
	for _, entry := range entries {
		if entry.IsDir() || !hasExtension(entry.Name(), ".json") {
			continue
		}

		_ = stripExtension(entry.Name()) // session ID (未使用但保留用于日志)

		// 快速读取基本信息
		filePath := filepath.Join(storageDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		if summary, err := decodeSessionSummary(data); err == nil {
			summaries = append(summaries, summary)
		}
	}

	// 按更新时间排序
	sortSummariesByTime(summaries)

	return summaries, nil
}

type sessionListEntry struct {
	ID           string           `json:"id"`
	CreatedAt    time.Time        `json:"createdAt"`
	UpdatedAt    time.Time        `json:"updatedAt"`
	MessageCount int              `json:"messageCount,omitempty"`
	Title        string           `json:"title,omitempty"`
	Path         string           `json:"path,omitempty"`
	Messages     []SessionMessage `json:"messages,omitempty"`
}

func decodeSessionSummary(data []byte) (SessionSummary, error) {
	var entry sessionListEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return SessionSummary{}, err
	}

	messageCount := entry.MessageCount
	if len(entry.Messages) > 0 {
		messageCount = len(entry.Messages)
	}

	return SessionSummary{
		ID:           entry.ID,
		CreatedAt:    entry.CreatedAt,
		UpdatedAt:    entry.UpdatedAt,
		MessageCount: messageCount,
		Title:        entry.Title,
		Path:         entry.Path,
	}, nil
}

// hasExtension 检查文件是否有指定扩展名
func hasExtension(name, ext string) bool {
	return len(name) >= len(ext) && name[len(name)-len(ext):] == ext
}

// sortSummariesByTime 按更新时间倒序排序
func sortSummariesByTime(s []SessionSummary) {
	for i := 0; i < len(s)-1; i++ {
		for j := 0; j < len(s)-i-1; j++ {
			if s[j].UpdatedAt.Before(s[j+1].UpdatedAt) {
				s[j], s[j+1] = s[j+1], s[j]
			}
		}
	}
}
