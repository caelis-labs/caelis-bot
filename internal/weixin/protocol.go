// Package weixin implements the text subset of Tencent/openclaw-weixin 2.4.9's
// published iLink protocol. See docs/weixin-channel-feasibility-2026-10-10.md.
// Copyright (c) Tencent. The upstream MIT license is in licenses/openclaw-weixin-LICENSE.
package weixin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const defaultBase = "https://ilinkai.weixin.qq.com"
const channelVersion = "2.4.9"
const clientVersion = "132105" // 0x00020409

type qrCode struct {
	Code    string `json:"qrcode"`
	Content string `json:"qrcode_img_content"`
}
type qrResult struct {
	Status       string `json:"status"`
	Token        string `json:"bot_token"`
	BotID        string `json:"ilink_bot_id"`
	UserID       string `json:"ilink_user_id"`
	BaseURL      string `json:"baseurl"`
	RedirectHost string `json:"redirect_host"`
}
type textItem struct {
	Text string `json:"text"`
}
type messageItem struct {
	Type int       `json:"type"`
	Text *textItem `json:"text_item,omitempty"`
	Ref  *refItem  `json:"ref_msg,omitempty"`
}
type refItem struct {
	ServerID wireID          `json:"svr_id"`
	Item     *messageItem    `json:"message_item,omitempty"`
	Title    string          `json:"title,omitempty"`
	Partial  json.RawMessage `json:"partial_text,omitempty"`
}
type message struct {
	MessageID    wireID        `json:"message_id"`
	Seq          json.Number   `json:"seq"`
	From         string        `json:"from_user_id"`
	To           string        `json:"to_user_id"`
	Group        string        `json:"group_id"`
	Type         int           `json:"message_type"`
	ContextToken string        `json:"context_token"`
	Items        []messageItem `json:"item_list"`
}

// The server may encode uint64 IDs as JSON numbers. Never round through float64.
type wireID string

func (id *wireID) UnmarshalJSON(raw []byte) error {
	if len(raw) == 0 || string(raw) == "null" {
		*id = ""
		return nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		*id = wireID(value)
		return nil
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return errors.New("invalid_message_id")
		}
	}
	*id = wireID(raw)
	return nil
}

type updates struct {
	Ret       int       `json:"ret"`
	ErrCode   int       `json:"errcode"`
	Messages  []message `json:"msgs"`
	Cursor    string    `json:"get_updates_buf"`
	TimeoutMS int       `json:"longpolling_timeout_ms"`
}
type sendResult struct {
	Ret       *int   `json:"ret"`
	ErrCode   int    `json:"errcode"`
	MessageID wireID `json:"message_id"`
}
type configResult struct {
	Ret          *int   `json:"ret"`
	TypingTicket string `json:"typing_ticket"`
}
type baseInfo struct {
	ChannelVersion string `json:"channel_version"`
	BotAgent       string `json:"bot_agent"`
}

func info() baseInfo { return baseInfo{channelVersion, "CaelisBot/POC"} }

type protocol struct {
	client *http.Client
	base   string
	token  string
}

func newProtocol(base, token string, client *http.Client) (*protocol, error) {
	if base == "" {
		base = defaultBase
	}
	if _, err := safeBase(base); err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	return &protocol{client: client, base: strings.TrimRight(base, "/"), token: token}, nil
}

// QR redirects and a returned API base are server supplied. Restrict them to
// Tencent's Weixin HTTPS domain before attaching a bearer token.
func safeBase(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return "", errors.New("untrusted_api_host")
	}
	host := strings.ToLower(u.Hostname())
	if host != "weixin.qq.com" && !strings.HasSuffix(host, ".weixin.qq.com") {
		return "", errors.New("untrusted_api_host")
	}
	return "https://" + host, nil
}
func (p *protocol) call(ctx context.Context, method, path string, body any, result any, auth bool) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", clientVersion)
	if method == http.MethodPost {
		var seed [4]byte
		if _, err = rand.Read(seed[:]); err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		req.Header.Set("X-WECHAT-UIN", base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(binary.BigEndian.Uint32(seed[:])), 10))))
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+p.token)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http_%d", resp.StatusCode)
	}
	if result == nil {
		return nil
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 2<<20))
	dec.UseNumber()
	if err := dec.Decode(result); err != nil {
		return err
	}
	return nil
}
func (p *protocol) qr(ctx context.Context) (qrCode, error) {
	var result qrCode
	err := p.call(ctx, http.MethodPost, "/ilink/bot/get_bot_qrcode?bot_type=3", map[string]any{"local_token_list": []string{}}, &result, false)
	if err == nil && (result.Code == "" || result.Content == "") {
		err = errors.New("invalid_qr_response")
	}
	return result, err
}
func (p *protocol) qrStatus(ctx context.Context, code, verify string) (qrResult, error) {
	query := url.Values{"qrcode": {code}}
	if verify != "" {
		query.Set("verify_code", verify)
	}
	var result qrResult
	err := p.call(ctx, http.MethodGet, "/ilink/bot/get_qrcode_status?"+query.Encode(), nil, &result, false)
	return result, err
}
func (p *protocol) getUpdates(ctx context.Context, cursor string) (updates, error) {
	var result updates
	err := p.call(ctx, http.MethodPost, "/ilink/bot/getupdates", map[string]any{"get_updates_buf": cursor, "base_info": info()}, &result, true)
	return result, err
}
func (p *protocol) send(ctx context.Context, owner, contextToken, clientID, text string) (sendResult, error) {
	msg := map[string]any{"from_user_id": "", "to_user_id": owner, "client_id": clientID, "message_type": 2, "message_state": 2, "item_list": []messageItem{{Type: 1, Text: &textItem{Text: text}}}}
	if contextToken != "" {
		msg["context_token"] = contextToken
	}
	var result sendResult
	err := p.call(ctx, http.MethodPost, "/ilink/bot/sendmessage", map[string]any{"msg": msg, "base_info": info()}, &result, true)
	return result, err
}
func (p *protocol) notify(ctx context.Context, started bool) error {
	path := "/ilink/bot/msg/notifystop"
	if started {
		path = "/ilink/bot/msg/notifystart"
	}
	var result sendResult
	if err := p.call(ctx, http.MethodPost, path, map[string]any{"base_info": info()}, &result, true); err != nil {
		return err
	}
	if result.Ret != nil && *result.Ret != 0 {
		return errors.New("notification_rejected")
	}
	return nil
}
func (p *protocol) getConfig(ctx context.Context, owner, contextToken string) (string, error) {
	var result configResult
	err := p.call(ctx, http.MethodPost, "/ilink/bot/getconfig", map[string]any{"ilink_user_id": owner, "context_token": contextToken, "base_info": info()}, &result, true)
	if err != nil {
		return "", err
	}
	if result.Ret == nil || *result.Ret != 0 {
		return "", errors.New("config_rejected")
	}
	return result.TypingTicket, nil
}
func (p *protocol) sendTyping(ctx context.Context, owner, ticket string, active bool) error {
	status := 2
	if active {
		status = 1
	}
	return p.call(ctx, http.MethodPost, "/ilink/bot/sendtyping", map[string]any{"ilink_user_id": owner, "typing_ticket": ticket, "status": status, "base_info": info()}, nil, true)
}
