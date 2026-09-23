package caelis

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/caelis/wire"
)

func (s *Session) UploadResource(ctx context.Context, op, name, media string, data []byte) (wire.ApplicationResource, error) {
	s.step.Lock()
	defer s.step.Unlock()
	s.mu.Lock()
	sid := s.state.Session.SessionId
	s.mu.Unlock()
	return s.uploadResource(ctx, sid, op, name, media, data)
}
func (s *Session) uploadResource(ctx context.Context, sid, op, name, media string, data []byte) (wire.ApplicationResource, error) {
	var out wire.ApplicationResource
	if len(data) > 8<<20 {
		return out, errors.New("附件超过 8 MB")
	}
	body := wire.ApplicationResourceRequest{OperationId: &op, SessionId: &sid, Name: name, MediaType: media, Data: base64.StdEncoding.EncodeToString(data), Sha256: digest(data)}
	path := "/application/sessions/" + idPath(sid) + "/resources"
	e := s.typedMutation(ctx, op, path, "", body, &out)
	if e != nil {
		return out, e
	}
	if out.SessionId != sid || out.Id == "" || out.Size != len(data) || out.Sha256 != digest(data) {
		return out, errors.New("Caelis 上传摘要不匹配")
	}
	return out, nil
}
func (s *Session) ReadResource(ctx context.Context, sid, id string) ([]byte, wire.ApplicationResource, error) {
	s.mu.Lock()
	c := s.client
	owned := sid == s.state.Session.SessionId
	for _, w := range s.state.Workers {
		if sid == w.Binding.SessionId {
			owned = true
		}
	}
	s.mu.Unlock()
	var result wire.ApplicationResourceContent
	if !owned || c == nil {
		return nil, result.Resource, errors.New("资源不属于当前 Bot")
	}
	e := c.json(ctx, "GET", "/application/sessions/"+idPath(sid)+"/resources/"+idPath(id)+"/content", nil, &result, "", "")
	if e != nil {
		return nil, result.Resource, e
	}
	data, e := base64.StdEncoding.DecodeString(result.Data)
	if e != nil || len(data) > 8<<20 || result.Resource.Size != len(data) || result.Resource.SessionId != sid || result.Resource.Id != id || result.Resource.Sha256 != digest(data) {
		return nil, result.Resource, errors.New("Caelis 下载完整性校验失败")
	}
	return data, result.Resource, nil
}

// Artifact resolves only links projected from canonical native tool output.
func (s *Session) Artifact(id string) (string, error) {
	s.mu.Lock()
	owned := false
	for _, v := range s.state.Views {
		for _, item := range v.Items {
			for _, a := range item.Artifacts {
				if a.ID == id {
					owned = true
				}
			}
		}
	}
	s.mu.Unlock()
	parts := strings.Split(id, ":")
	if !owned || len(parts) != 3 || parts[0] != "resource" {
		return "", errors.New("未知的文件")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	data, resource, e := s.ReadResource(ctx, parts[1], parts[2])
	if e != nil {
		return "", e
	}
	dir := filepath.Join(filepath.Dir(s.path), "downloads")
	if e = os.MkdirAll(dir, 0700); e != nil {
		return "", e
	}
	if e = privateDir(dir); e != nil {
		return "", e
	}
	name := filepath.Base(resource.Name)
	if name == "." || name == string(filepath.Separator) {
		name = "artifact"
	}
	target := filepath.Join(dir, digest([]byte(id))[:16]+"-"+name)
	f, e := os.CreateTemp(dir, ".download-*")
	if e != nil {
		return "", e
	}
	defer os.Remove(f.Name())
	_, e = f.Write(data)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		return "", e
	}
	if e = os.Rename(f.Name(), target); e != nil {
		return "", e
	}
	return target, nil
}
