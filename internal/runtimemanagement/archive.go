package runtimemanagement

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

const maxArchive = 256 << 20
const maxExpanded = 1 << 30

func officialDownload(ctx context.Context, release Release) (io.ReadCloser, error) {
	u, err := url.Parse(release.URL)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return nil, errors.New("invalid official release URL")
	}
	allowed := func(host string) bool {
		switch host {
		case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "releases.caelis.dev":
			return true
		}
		return false
	}
	if !allowed(u.Hostname()) {
		return nil, errors.New("unsupported release source")
	}
	client := &http.Client{Timeout: 4 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 4 || req.URL.Scheme != "https" || req.URL.User != nil || !allowed(req.URL.Hostname()) {
			return errors.New("unsupported release redirect")
		}
		return nil
	}}
	req, err := http.NewRequestWithContext(ctx, "GET", release.URL, nil)
	if err != nil {
		return nil, errors.New("invalid release request")
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("official release download failed")
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > maxArchive || (release.Size > 0 && response.ContentLength >= 0 && response.ContentLength != release.Size) {
		_ = response.Body.Close()
		return nil, errors.New("official release response invalid")
	}
	return response.Body, nil
}

func saveArchive(ctx context.Context, root *os.Root, name string, release Release, source io.Reader) error {
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("could not stage release archive")
	}
	defer file.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(contextReader{ctx, source}, maxArchive+1))
	if err != nil || n > maxArchive || (release.Size > 0 && n != release.Size) {
		return errors.New("release archive incomplete")
	}
	if hex.EncodeToString(hash.Sum(nil)) != release.SHA256 {
		return errors.New("release checksum mismatch")
	}
	if err := file.Sync(); err != nil {
		return errors.New("could not persist release archive")
	}
	return nil
}

type contextReader struct {
	context context.Context
	source  io.Reader
}

func (r contextReader) Read(b []byte) (int, error) {
	if err := r.context.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(b)
}

func archiveName(name string) (string, error) {
	name = strings.TrimPrefix(name, "./")
	name = strings.TrimSuffix(name, "/")
	if name == "" || name == "." {
		return ".", nil
	}
	if strings.ContainsAny(name, "\\\x00\r\n") || strings.HasPrefix(name, "/") || path.Clean(name) != name {
		return "", errors.New("unsafe release archive path")
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." || part == "" {
			return "", errors.New("unsafe release archive path")
		}
	}
	return name, nil
}

func extractArchive(ctx context.Context, root *os.Root, archive, directory string) error {
	file, err := root.Open(archive)
	if err != nil {
		return errors.New("could not open staged release")
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return errors.New("invalid release compression")
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	extracted, err := root.OpenRoot(directory)
	if err != nil {
		return errors.New("could not open release staging directory")
	}
	defer extracted.Close()
	var expanded int64
	seen := map[string]bool{}
	for entries := 0; ; entries++ {
		if entries > 4096 {
			return errors.New("release archive has too many entries")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.New("invalid release archive")
		}
		name, err := archiveName(header.Name)
		if err != nil {
			return err
		}
		if name == "." && header.Typeflag == tar.TypeDir {
			continue
		}
		if name == "." || seen[name] {
			return errors.New("duplicate release archive path")
		}
		seen[name] = true
		switch header.Typeflag {
		case tar.TypeDir:
			if err := extracted.MkdirAll(name, 0700); err != nil {
				return errors.New("could not extract release directory")
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxExpanded || expanded > maxExpanded-header.Size {
				return errors.New("release expanded size exceeds limit")
			}
			expanded += header.Size
			if err := extracted.MkdirAll(path.Dir(name), 0700); err != nil {
				return errors.New("could not extract release directory")
			}
			mode := os.FileMode(0600)
			if header.Mode&0111 != 0 {
				mode = 0700
			}
			output, err := extracted.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
			if err != nil {
				return errors.New("could not extract release file")
			}
			_, err = io.CopyN(output, contextReader{ctx, reader}, header.Size)
			if err == nil {
				err = output.Sync()
			}
			closed := output.Close()
			if err != nil || closed != nil {
				return errors.New("release extraction incomplete")
			}
		default:
			return errors.New("release archive links and special files are unsupported")
		}
	}
	// Reading gzip to EOF also verifies its trailer instead of accepting a
	// truncated archive whose tar end marker arrived before the compression tail.
	if _, err := io.Copy(io.Discard, gzipReader); err != nil {
		return errors.New("release compression incomplete")
	}
	return nil
}
