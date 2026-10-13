package weixin

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
)

// The native file item has no MIME field. Use the generic FILE media type for
// every regular artifact, including images, so the receiver gets exact bytes.
const maxOutgoingFile = 20 << 20

func artifactKey(id string) string { return "artifact:" + id }

func (b *Bridge) setFileIssue(reason string) {
	b.mu.Lock()
	if b.state.FileIssue != reason {
		b.state.FileIssue = reason
		_ = b.saveLocked()
	}
	b.issue = reason
	b.mu.Unlock()
}

func fileName(name string) bool {
	if name == "" || name == "." || name == ".." || name != filepath.Base(name) || len(name) > 255 || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '\\' {
			return false
		}
	}
	return true
}

func readArtifact(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("file_unavailable")
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() || before.Size() > maxOutgoingFile {
		return nil, errors.New("file_unsupported")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxOutgoingFile+1))
	after, statErr := f.Stat()
	if err != nil || statErr != nil || len(data) > maxOutgoingFile || int64(len(data)) != before.Size() || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		return nil, errors.New("file_changed")
	}
	return data, nil
}

func encryptFile(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padding := aes.BlockSize - len(plain)%aes.BlockSize
	result := make([]byte, len(plain)+padding)
	copy(result, plain)
	for i := len(plain); i < len(result); i++ {
		result[i] = byte(padding)
	}
	for i := 0; i < len(result); i += aes.BlockSize {
		block.Encrypt(result[i:i+aes.BlockSize], result[i:i+aes.BlockSize])
	}
	return result, nil
}

type uploadURLResult struct {
	Ret     *int   `json:"ret"`
	ErrCode int    `json:"errcode"`
	Param   string `json:"upload_param"`
	FullURL string `json:"upload_full_url"`
}

// A returned full URL is untrusted. The CDN request carries encrypted bytes,
// but a redirect or foreign host must never receive them (or the AES key).
func (p *protocol) uploadURL(result uploadURLResult, fileKey string) (string, error) {
	base := p.cdnBase
	if base == "" {
		base = "https://novac2c.cdn.weixin.qq.com/c2c"
	}
	target := strings.TrimSpace(result.FullURL)
	if len(target) > 8<<10 || len(result.Param) > 8<<10 {
		return "", errors.New("upload_url_untrusted")
	}
	if target == "" && result.Param != "" {
		target = strings.TrimRight(base, "/") + "/upload?encrypted_query_param=" + url.QueryEscape(result.Param) + "&filekey=" + url.QueryEscape(fileKey)
	}
	u, err := url.Parse(target)
	allowed, baseErr := url.Parse(base)
	if err != nil || baseErr != nil || u.User != nil || u.Fragment != "" || u.Scheme != "https" || u.Host != allowed.Host || !strings.HasPrefix(u.EscapedPath(), strings.TrimRight(allowed.EscapedPath(), "/")+"/") || u.EscapedPath() != strings.TrimRight(allowed.EscapedPath(), "/")+"/upload" {
		return "", errors.New("upload_url_untrusted")
	}
	return u.String(), nil
}

func (p *protocol) uploadFile(ctx context.Context, owner string, plain []byte) (fileItem, string) {
	if ctx.Err() != nil {
		return fileItem{}, "upload_cancelled"
	}
	var aesKey, fileKey [16]byte
	if _, err := rand.Read(aesKey[:]); err != nil {
		return fileItem{}, "upload_unavailable"
	}
	if _, err := rand.Read(fileKey[:]); err != nil {
		return fileItem{}, "upload_unavailable"
	}
	ciphertext, err := encryptFile(plain, aesKey[:])
	if err != nil {
		return fileItem{}, "upload_unavailable"
	}
	key := hex.EncodeToString(fileKey[:])
	checksum := md5.Sum(plain) // Required by Tencent's upload protocol, not an authorization hash.
	var response uploadURLResult
	err = p.call(ctx, http.MethodPost, "/ilink/bot/getuploadurl", map[string]any{
		"filekey": key, "media_type": 3, "to_user_id": owner,
		"rawsize": len(plain), "rawfilemd5": hex.EncodeToString(checksum[:]),
		"filesize": len(ciphertext), "no_need_thumb": true,
		"aeskey": hex.EncodeToString(aesKey[:]), "base_info": info(),
	}, &response, true)
	if err != nil {
		if ctx.Err() != nil {
			return fileItem{}, "upload_cancelled"
		}
		return fileItem{}, "upload_unknown"
	}
	if response.Ret != nil && *response.Ret != 0 || response.ErrCode != 0 {
		return fileItem{}, "upload_rejected"
	}
	target, err := p.uploadURL(response, key)
	if err != nil {
		return fileItem{}, "upload_untrusted"
	}
	client := *p.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return fileItem{}, "upload_cancelled"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(ciphertext))
		if err != nil {
			return fileItem{}, "upload_unavailable"
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		res, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return fileItem{}, "upload_cancelled"
			}
			continue // No message has been attempted; retrying encrypted CDN bytes is safe.
		}
		param := res.Header.Get("x-encrypted-param")
		_ = res.Body.Close()
		if res.StatusCode >= 400 && res.StatusCode < 500 {
			return fileItem{}, "upload_rejected"
		}
		if res.StatusCode == http.StatusOK && param != "" && len(param) <= 8<<10 {
			return fileItem{Media: cdnMedia{QueryParam: param, AESKey: base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(aesKey[:]))), Type: 1}, Len: strconv.Itoa(len(plain))}, ""
		}
	}
	return fileItem{}, "upload_unknown"
}

func (b *Bridge) scheduleArtifact(ctx context.Context, p *protocol, artifact api.Artifact, critical bool) {
	if artifact.ID == "" || !fileName(artifact.Name) {
		b.setFileIssue("file_unsupported")
		return
	}
	key := artifactKey(artifact.ID)
	b.mu.Lock()
	if b.fileWork == nil {
		b.fileWork = map[string]bool{}
	}
	old, seen := b.state.Outputs[key]
	if seen && old.State != "pending" && old.State != "preparing" {
		b.mu.Unlock()
		return
	}
	if seen && old.FileName != "" {
		artifact.Name = old.FileName // keep the first projected delivery name
	}
	critical = critical || old.FileCritical
	if !seen {
		old = outbound{State: "pending", FileName: artifact.Name, FileCritical: critical, Digest: textDigest(artifact.ID)}
		b.state.Outputs[key] = old
		if b.saveLocked() != nil {
			delete(b.state.Outputs, key)
			b.mu.Unlock()
			return
		}
	} else if critical && !old.FileCritical {
		old.FileCritical = true
		b.state.Outputs[key] = old
		if b.saveLocked() != nil {
			old.FileCritical = false
			b.state.Outputs[key] = old
			b.mu.Unlock()
			return
		}
	}
	if b.fileWork[key] || len(b.fileWork) >= 2 {
		b.mu.Unlock()
		return
	}
	if b.remainingLocked(critical) == 0 {
		b.issue = "file_deferred"
		if b.state.FileIssue != "file_deferred" {
			b.state.FileIssue = "file_deferred"
			_ = b.saveLocked()
		}
		b.mu.Unlock()
		return
	}
	if b.host.Artifact == nil {
		b.issue = "file_unavailable"
		if b.state.FileIssue != "file_unavailable" {
			b.state.FileIssue = "file_unavailable"
			_ = b.saveLocked()
		}
		b.mu.Unlock()
		return
	}
	if old.State == "pending" {
		preparing := old
		preparing.State = "preparing"
		b.state.Outputs[key] = preparing
		if b.saveLocked() != nil {
			b.state.Outputs[key] = old
			b.mu.Unlock()
			return
		}
	}
	b.fileWork[key] = true
	b.mu.Unlock()
	go func() {
		defer func() { b.mu.Lock(); delete(b.fileWork, key); b.mu.Unlock() }()
		b.deliverArtifact(ctx, p, artifact, key, critical)
	}()
}

// Pending records contain only previously authorized Artifact handles and
// filenames. Re-resolve each handle through Host; never persist a local path.
func (b *Bridge) flushPendingArtifacts(ctx context.Context, p *protocol) {
	type pending struct {
		artifact api.Artifact
		critical bool
	}
	b.mu.Lock()
	files := make([]pending, 0)
	for key, record := range b.state.Outputs {
		if strings.HasPrefix(key, "artifact:") && (record.State == "pending" || record.State == "preparing") && record.FileName != "" {
			files = append(files, pending{api.Artifact{ID: strings.TrimPrefix(key, "artifact:"), Name: record.FileName}, record.FileCritical})
		}
	}
	b.mu.Unlock()
	for _, file := range files {
		b.scheduleArtifact(ctx, p, file.artifact, file.critical)
	}
}

func (b *Bridge) failArtifact(key, reason string) {
	b.mu.Lock()
	record := b.state.Outputs[key]
	if record.State == "preparing" {
		record.State, record.Reason = reason, reason
		b.state.Outputs[key] = record
		b.issue = reason
		b.state.FileIssue = reason
		_ = b.saveLocked()
	}
	b.mu.Unlock()
}

func (b *Bridge) deliverArtifact(ctx context.Context, p *protocol, artifact api.Artifact, key string, critical bool) {
	work, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	path, err := b.host.Artifact(artifact.ID) // Host resolver enforces the same owned-ID boundary as Telegram.
	if work.Err() != nil {
		return
	}
	if err != nil {
		b.failArtifact(key, "file_unavailable")
		return
	}
	plain, err := readArtifact(path)
	if work.Err() != nil {
		return
	}
	if err != nil {
		b.failArtifact(key, err.Error())
		return
	}
	b.mu.Lock()
	owner := b.state.OwnerID
	b.mu.Unlock()
	file, reason := p.uploadFile(work, owner, plain)
	if reason != "" {
		if reason != "upload_cancelled" {
			b.failArtifact(key, reason)
		}
		return // An interrupted preparation may be safely resumed after restart.
	}
	file.Name = artifact.Name
	b.sendFileOne(work, p, key, file, critical)
}

// Only this transition spends a send slot. Persisting unknown before HTTP
// means a crash, cancellation, or ambiguous response cannot replay the file.
func (b *Bridge) sendFileOne(ctx context.Context, p *protocol, key string, file fileItem, critical bool) {
	if ctx.Err() != nil {
		return
	}
	var seed [16]byte
	if _, err := rand.Read(seed[:]); err != nil {
		b.failArtifact(key, "file_unavailable")
		return
	}
	b.mu.Lock()
	prior := b.state.Outputs[key]
	if prior.State != "preparing" {
		b.mu.Unlock()
		return
	}
	if b.remainingLocked(critical) == 0 {
		b.issue = "file_deferred"
		b.state.FileIssue = "file_deferred"
		_ = b.saveLocked()
		b.mu.Unlock()
		return
	}
	window := b.state.Window
	b.state.Window.Used++
	w := b.state.Window
	size, _ := strconv.Atoi(file.Len)
	intent := outbound{State: "unknown", ClientID: "caelis-weixin-" + hex.EncodeToString(seed[:]), ContextToken: w.ContextToken, Digest: prior.Digest, Attempts: 1, Bytes: size, InputAgeSec: int64(time.Since(time.UnixMilli(w.InputAt)).Seconds()), WindowUsed: w.Used}
	b.state.Outputs[key] = intent
	if b.saveLocked() != nil {
		b.state.Window = window
		b.state.Outputs[key] = prior
		b.mu.Unlock()
		return
	}
	owner := b.state.OwnerID
	b.mu.Unlock()
	work, cancel := context.WithTimeout(ctx, 15*time.Second)
	result, err := p.sendItem(work, owner, intent.ContextToken, intent.ClientID, messageItem{Type: 4, File: &file})
	cancel()
	b.mu.Lock()
	state, reason := "accepted", "ret_zero"
	if err != nil {
		state, reason = "unknown", "transport_or_response"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "timeout"
		}
		b.issue = "file_delivery_uncertain"
		b.state.FileIssue = b.issue
	} else {
		intent.Ret, intent.ErrCode = result.Ret, &result.ErrCode
		intent.ErrorClass = sendErrorClass(result.ErrMsg)
		if result.Ret == nil && result.ErrCode == 0 && intent.ErrorClass == "other" {
			state, reason = "unknown", "unclassified_response"
			b.issue = "file_delivery_uncertain"
			b.state.FileIssue = b.issue
		} else if result.Ret != nil && *result.Ret != 0 || result.ErrCode != 0 || intent.ErrorClass != "" && intent.ErrorClass != "other" {
			state, reason = "rejected", "business_rejected"
			b.issue = "file_send_rejected"
			b.state.FileIssue = b.issue
			if result.Ret != nil && *result.Ret == -14 || result.ErrCode == -14 || intent.ErrorClass == "authentication" {
				b.issue = "auth_expired"
				b.state.FileIssue = "auth_expired"
			}
			if b.state.Window.InputID == w.InputID && (intent.ErrorClass == "rate_limited" || intent.ErrorClass == "context_expired") {
				b.state.Window.Exhausted = true
			}
		} else {
			intent.MessageID = string(result.MessageID)
			if result.Ret == nil {
				reason = "http_success_no_ret"
			}
		}
	}
	intent.State, intent.Reason = state, reason
	if state == "accepted" && b.state.FileIssue == "file_deferred" {
		b.state.FileIssue = ""
		if b.issue == "file_deferred" {
			b.issue = ""
		}
	}
	b.state.Outputs[key] = intent
	_ = b.saveLocked()
	b.mu.Unlock()
}
