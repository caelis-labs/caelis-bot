// caelis-remote is a headless, per-connection owner. No login credentials or
// resident Bot state are accepted from the controlling computer.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/caelis-labs/caelis-bot/internal/remotework"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) != 3 {
		os.Exit(2)
	}
	mode, root := os.Args[1], os.Args[2]
	if !filepath.IsAbs(root) {
		os.Exit(2)
	}
	socket := remotework.Socket(root)
	if e := os.MkdirAll(filepath.Dir(socket), 0700); e != nil {
		os.Exit(1)
	}
	info, e := os.Lstat(filepath.Dir(socket))
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		os.Exit(1)
	}
	if mode == "serve" {
		serve(root, socket)
		return
	}
	if mode != "proxy" {
		os.Exit(2)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		os.Exit(1)
	}
	lock, e := os.OpenFile(filepath.Join(root, "owner.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		os.Exit(1)
	}
	defer lock.Close()
	if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) != nil {
		os.Exit(1)
	}
	if c, e := net.DialTimeout("unix", socket, time.Second); e == nil {
		c.Close()
	} else {
		exe, _ := os.Executable()
		log, e := os.OpenFile(filepath.Join(root, "owner.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			os.Exit(1)
		}
		cmd := exec.Command(exe, "serve", root)
		cmd.Stdout = log
		cmd.Stderr = log
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if cmd.Start() != nil {
			os.Exit(1)
		}
		log.Close()
		_ = cmd.Process.Release()
		ready := false
		for i := 0; i < 100; i++ {
			if c, e := net.DialTimeout("unix", socket, time.Millisecond*100); e == nil {
				c.Close()
				ready = true
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		if !ready {
			os.Exit(1)
		}
	}
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}
	client := &http.Client{Transport: tr, Timeout: 50 * time.Second}
	scan := bufio.NewScanner(os.Stdin)
	scan.Buffer(make([]byte, 4096), 128*1024)
	for scan.Scan() {
		res, e := client.Post("http://owner/operation", "application/json", strings.NewReader(scan.Text()))
		if e != nil {
			os.Exit(1)
		}
		b, e := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		res.Body.Close()
		if e != nil || res.StatusCode != 200 {
			os.Exit(1)
		}
		fmt.Println(string(b))
	}
}
func serve(root, socket string) {
	owner, e := remotework.New(root)
	if e != nil {
		os.Exit(1)
	}
	lock, e := os.OpenFile(filepath.Join(root, "lifetime.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil || syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		os.Exit(1)
	}
	defer lock.Close()
	if i, e := os.Lstat(socket); e == nil {
		if i.Mode()&os.ModeSocket == 0 {
			os.Exit(1)
		}
		os.Remove(socket)
	}
	l, e := net.Listen("unix", socket)
	if e != nil {
		os.Exit(1)
	}
	defer l.Close()
	os.Chmod(socket, 0600)
	s := &http.Server{ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/operation" {
			http.NotFound(w, r)
			return
		}
		var v remotework.Request
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128<<10))
		dec.DisallowUnknownFields()
		if dec.Decode(&v) != nil {
			http.Error(w, "invalid request", 400)
			return
		}
		ctx, c := context.WithTimeout(r.Context(), 45*time.Second)
		defer c()
		out := owner.Handle(ctx, v)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	})}
	_ = s.Serve(l)
}
