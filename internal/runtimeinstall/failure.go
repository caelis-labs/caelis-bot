package runtimeinstall

import (
	"bytes"
	"context"
	"crypto/x509"
	"errors"
	"io/fs"
	"net"
	"strings"
	"syscall"
)

// Failure contains only allowlisted categories and numeric status. Native
// stderr is bounded in memory and classified, never persisted or displayed.
type Failure struct {
	Code                 string
	ExitCode, HTTPStatus int
}

func (f *Failure) Error() string {
	switch f.Code {
	case "download_http":
		return "官方下载服务暂未提供安装文件，请稍后重试"
	case "download_tls":
		return "无法验证官方下载服务的安全证书，请检查服务端证书设置"
	case "download_timeout":
		return "官方下载超时，请稍后重试"
	case "download_network":
		return "无法连接官方下载服务，请检查服务端网络后重试"
	case "installation_permission":
		return "无法写入服务端安装目录，请检查该用户的目录权限"
	case "storage_unavailable":
		return "服务端磁盘空间不足，请释放空间后重试"
	case "missing_dependency":
		return "服务端缺少安装所需的系统工具，请修复后重试"
	case "archive_invalid":
		return "下载文件未通过校验，请稍后重新下载"
	case "cancelled":
		return "安装已取消"
	case "unsupported_platform":
		return "此平台暂未支持自动安装"
	case "installer_exit":
		return "官方安装程序未能完成，请稍后重试"
	default:
		return "安装未完成，原因尚未确认；请查看安装诊断"
	}
}

func Classify(err error) *Failure {
	var classified *Failure
	if errors.As(err, &classified) {
		return classified
	}
	var cert x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var timeout net.Error
	switch {
	case errors.Is(err, context.Canceled):
		return &Failure{Code: "cancelled"}
	case errors.Is(err, fs.ErrPermission):
		return &Failure{Code: "installation_permission"}
	case errors.Is(err, syscall.ENOSPC):
		return &Failure{Code: "storage_unavailable"}
	case errors.As(err, &cert), errors.As(err, &invalid):
		return &Failure{Code: "download_tls"}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &timeout) && timeout.Timeout():
		return &Failure{Code: "download_timeout"}
	}
	return &Failure{Code: "unclassified"}
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := 16384 - b.Len(); remaining > 0 {
		_, _ = b.Buffer.Write(p[:min(remaining, n)])
	}
	return n, nil
}

func classifyInstallerExit(stderr string, code int) *Failure {
	f := &Failure{Code: "installer_exit", ExitCode: code}
	text := strings.ToLower(stderr)
	for _, rule := range []struct{ fragment, code string }{
		{"no space left on device", "storage_unavailable"},
		{"permission denied", "installation_permission"},
		{"checksum mismatch", "archive_invalid"},
		{"checksum verification failed", "archive_invalid"},
		{"certificate", "download_tls"},
		{"could not resolve", "download_network"},
		{"failed to connect", "download_network"},
		{"connection refused", "download_network"},
		{"timed out", "download_timeout"},
		{"command not found", "missing_dependency"},
		{"required command", "missing_dependency"},
	} {
		if strings.Contains(text, rule.fragment) {
			f.Code = rule.code
			break
		}
	}
	return f
}
