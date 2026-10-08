package plugins

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// relayRemote is a deliberately narrow, lossless JSON-RPC transport bridge.
// It never interprets or retries tools/call. A lost response remains unknown
// to the Runtime, which owns its original call receipt.
func relayRemote(server Server, spec *ConnectionSpec, secret, caPEM string, in io.Reader, out io.Writer) error {
	return relayRemoteContext(context.Background(), server, spec, secret, caPEM, in, out)
}

func relayRemoteContext(parent context.Context, server Server, spec *ConnectionSpec, secret, caPEM string, in io.Reader, out io.Writer) error {
	u, err := url.Parse(server.URL)
	if err != nil {
		return errors.New("invalid MCP endpoint")
	}
	if spec != nil && spec.Placement == "query" {
		q := u.Query()
		q.Set(spec.Name, secret)
		u.RawQuery = q.Encode()
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if caPEM != "" {
		pool, _ := x509.SystemCertPool()
		if pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(caPEM)) {
			return errors.New("invalid trusted CA")
		}
		transport.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Minute, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer transport.CloseIdleConnections()
	reader := bufio.NewScanner(in)
	reader.Buffer(make([]byte, 64<<10), 16<<20)
	written := bufio.NewWriter(out)
	defer written.Flush()
	session, protocol := "", "2025-06-18"
	for reader.Scan() {
		line := append([]byte(nil), reader.Bytes()...)
		var msg struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			ID      json.RawMessage `json:"id"`
		}
		if json.Unmarshal(line, &msg) != nil || msg.JSONRPC != "2.0" {
			continue
		}
		ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(line))
		if err != nil {
			cancel()
			return errors.New("invalid MCP request")
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("MCP-Protocol-Version", protocol)
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		for k, v := range server.Headers {
			req.Header.Set(k, v)
		}
		if spec != nil && spec.Placement == "header" {
			req.Header.Set(spec.Name, spec.Prefix+secret)
		}
		response, err := client.Do(req)
		if err != nil {
			cancel()
			if len(msg.ID) > 0 {
				writeRelayError(written, msg.ID, "MCP connection failed")
			}
			continue
		}
		if msg.Method == "initialize" {
			if id := response.Header.Get("Mcp-Session-Id"); len(id) <= 512 {
				session = id
			}
		}
		if response.StatusCode >= 400 {
			io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
			response.Body.Close()
			cancel()
			if len(msg.ID) > 0 {
				if response.StatusCode == 401 || response.StatusCode == 403 {
					writeRelayError(written, msg.ID, "MCP authentication failed")
				} else {
					writeRelayError(written, msg.ID, fmt.Sprintf("MCP server returned HTTP %d", response.StatusCode))
				}
			}
			continue
		}
		ct := response.Header.Get("Content-Type")
		if strings.HasPrefix(ct, "text/event-stream") {
			scan := bufio.NewScanner(response.Body)
			scan.Buffer(make([]byte, 64<<10), 16<<20)
			var data []string
			flush := func() {
				if len(data) > 0 {
					payload := strings.Join(data, "\n")
					if json.Valid([]byte(payload)) {
						var compact bytes.Buffer
						if json.Compact(&compact, []byte(payload)) != nil {
							data = nil
							return
						}
						if msg.Method == "initialize" {
							protocol = negotiatedProtocol(payload, protocol)
						}
						written.Write(compact.Bytes())
						written.WriteByte('\n')
						written.Flush()
					}
					data = nil
				}
			}
			for scan.Scan() {
				v := scan.Text()
				if v == "" {
					flush()
				} else if strings.HasPrefix(v, "data:") {
					data = append(data, strings.TrimPrefix(strings.TrimPrefix(v, "data:"), " "))
				}
			}
			flush()
		} else if strings.HasPrefix(ct, "application/json") {
			body, e := io.ReadAll(io.LimitReader(response.Body, 16<<20+1))
			if e == nil && len(body) <= 16<<20 && json.Valid(body) {
				var compact bytes.Buffer
				if json.Compact(&compact, body) != nil {
					response.Body.Close()
					cancel()
					return errors.New("MCP response invalid")
				}
				if msg.Method == "initialize" {
					protocol = negotiatedProtocol(string(body), protocol)
				}
				written.Write(compact.Bytes())
				written.WriteByte('\n')
				written.Flush()
			} else if len(msg.ID) > 0 {
				writeRelayError(written, msg.ID, "MCP response invalid")
			}
		} else if len(msg.ID) > 0 {
			writeRelayError(written, msg.ID, "MCP response type unsupported")
		}
		response.Body.Close()
		cancel()
	}
	return reader.Err()
}

func negotiatedProtocol(body, fallback string) string {
	var response struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
		} `json:"result"`
	}
	if json.Unmarshal([]byte(body), &response) != nil {
		return fallback
	}
	v := response.Result.ProtocolVersion
	if len(v) != 10 || v[4] != '-' || v[7] != '-' {
		return fallback
	}
	for i, r := range v {
		if i != 4 && i != 7 && (r < '0' || r > '9') {
			return fallback
		}
	}
	return v
}

func writeRelayError(out *bufio.Writer, id json.RawMessage, message string) {
	if !json.Valid(id) {
		return
	}
	b, _ := json.Marshal(message)
	out.WriteString(`{"jsonrpc":"2.0","id":`)
	out.Write(id)
	out.WriteString(`,"error":{"code":-32000,"message":`)
	out.Write(b)
	out.WriteString("}}\n")
	out.Flush()
}
