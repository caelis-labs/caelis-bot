package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

const attachmentRetention = 30 * 24 * time.Hour

var cacheDirectoryName = regexp.MustCompile(`^input-[0-9]+$`)

type cacheEntry struct {
	path   string
	info   os.FileInfo
	files  int
	bytes  int64
	latest time.Time
}

// Only host-created, flat directories of ordinary copies are eligible.
// Never follow symlinks or traverse the user's source/results directories.
func attachmentEntries(directory string) ([]cacheEntry, error) {
	root := filepath.Join(directory, ".attachments")
	info, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("附件存储暂时无法读取")
	}
	children, err := os.ReadDir(root)
	if err != nil {
		return nil, errors.New("附件存储暂时无法读取")
	}
	var result []cacheEntry
	for _, child := range children {
		if !cacheDirectoryName.MatchString(child.Name()) || !child.IsDir() {
			continue
		}
		path := filepath.Join(root, child.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		entry := cacheEntry{path: path, info: info, latest: info.ModTime()}
		files, err := os.ReadDir(path)
		if err != nil {
			return nil, errors.New("部分附件副本暂时无法读取")
		}
		safe := true
		for _, file := range files {
			fi, err := file.Info()
			if err != nil || !fi.Mode().IsRegular() {
				safe = false
				break
			}
			entry.files++
			entry.bytes += fi.Size()
			if fi.ModTime().After(entry.latest) {
				entry.latest = fi.ModTime()
			}
		}
		if safe {
			result = append(result, entry)
		}
	}
	return result, nil
}
func (s *Session) maintenanceIdle() bool {
	return s.state.Connection == "ready" && s.binding.Pending == nil && s.run == "" && len(s.childRuns) == 0 && len(s.prompts) == 0 && s.state.Phase != "unknown" && !s.closed && !s.closing
}
func (s *Session) AttachmentStorage() (api.AttachmentStorage, error) {
	entries, err := attachmentEntries(s.opts.Directory)
	if err != nil {
		return api.AttachmentStorage{}, err
	}
	var info api.AttachmentStorage
	cutoff := time.Now().Add(-attachmentRetention)
	for _, entry := range entries {
		info.Files += entry.files
		info.Bytes += entry.bytes
		if entry.latest.Before(cutoff) {
			info.EligibleFiles += entry.files
			info.EligibleBytes += entry.bytes
		}
	}
	s.mu.Lock()
	idle := s.maintenanceIdle()
	s.mu.Unlock()
	info.CanClean = idle && info.EligibleFiles > 0
	if !idle {
		info.Notice = "连接就绪且工作结束后才能清理。"
	}
	return info, nil
}
func (s *Session) TrashOldAttachments(ctx context.Context, trash func(string) error) (api.AttachmentStorage, error) {
	s.op.Lock()
	defer s.op.Unlock()
	ctx, cancel := s.operation(ctx, 15*time.Second)
	defer cancel()
	s.mu.Lock()
	idle, c, thread := s.maintenanceIdle(), s.client, s.binding.ThreadID
	ids := []string{thread}
	for child := range s.children {
		ids = append(ids, child)
	}
	s.mu.Unlock()
	if !idle || c == nil || trash == nil {
		return api.AttachmentStorage{}, errors.New("请在连接就绪且工作结束后清理")
	}
	// Completed model turns may still own background terminals.
	for _, id := range ids {
		var terminals struct {
			Data       []any  `json:"data"`
			NextCursor string `json:"nextCursor"`
		}
		if callDecode(ctx, c, "thread/backgroundTerminals/list", map[string]any{"threadId": id, "limit": 1}, &terminals) != nil || terminals.Data == nil {
			return api.AttachmentStorage{}, errors.New("暂时无法确认后台工作状态，未清理附件")
		}
		if len(terminals.Data) > 0 || terminals.NextCursor != "" {
			return api.AttachmentStorage{}, errors.New("仍有后台工具运行，请结束后再清理")
		}
	}
	entries, err := attachmentEntries(s.opts.Directory)
	if err != nil {
		return api.AttachmentStorage{}, err
	}
	cutoff := time.Now().Add(-attachmentRetention)
	for _, entry := range entries {
		if !entry.latest.Before(cutoff) {
			continue
		}
		s.mu.Lock()
		idle = s.maintenanceIdle()
		s.mu.Unlock()
		if !idle {
			return api.AttachmentStorage{}, errors.New("有新工作开始，已停止继续清理")
		}
		current, err := os.Lstat(entry.path)
		if err != nil || !os.SameFile(entry.info, current) || !current.IsDir() {
			return api.AttachmentStorage{}, errors.New("附件存储已变化，请刷新后重试")
		}
		if err = trash(entry.path); err != nil {
			return api.AttachmentStorage{}, errors.New("部分附件未能移到废纸篓，请刷新后重试")
		}
	}
	return s.AttachmentStorage()
}
