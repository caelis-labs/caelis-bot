// Package workerurl transports one original native loopback HTTP form. It has
// no native decision authority, HTTP proxy route, saved browser profile or log.
package workerurl

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/backend/api"
	"golang.org/x/net/html"
)

const MaxPageBytes = 64 << 10

var ErrUnavailable = errors.New("worker_url_page_unavailable")
var ErrUnsupported = errors.New("worker_url_page_unsupported")
var ErrInvalid = errors.New("worker_url_input_invalid")
var namePattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]{1,80}$`)

// Page is ephemeral transport state for a single original native request. The
// owning adapter keeps durable ID/digest/outcome in its existing receipt map.
// None of the page, cookie jar, hidden values or user fields are persisted.
type Page struct {
	View   api.WorkURLPage
	target *url.URL
	hidden url.Values
	client *http.Client
}

func targetURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.Port() == "" {
		return nil, ErrUnsupported
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, ErrUnsupported
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.IsLoopback() {
		return nil, ErrUnsupported
	} // no DNS, .local or private-network expansion
	if addr, ok := ctx.Value(http.LocalAddrContextKey).(net.Addr); ok {
		_, port, e := net.SplitHostPort(addr.String())
		blockedPort, _ := strconv.Atoi(port)
		if e != nil || blockedPort == mustPort(u) {
			return nil, ErrUnsupported
		} // never the receiving Bot/peer/control listener
	}
	u.Host = net.JoinHostPort(u.Hostname(), strconv.Itoa(port))
	return u, nil
}
func mustPort(u *url.URL) int { n, _ := strconv.Atoi(u.Port()); return n }
func Fetch(ctx context.Context, raw string) (*Page, error) {
	u, err := targetURL(ctx, raw)
	if err != nil {
		return nil, err
	}
	jar, _ := cookiejar.New(nil)
	// No environment proxy, inherited cookies, Authorization or custom headers.
	// Only the literal original loopback endpoint is dialed, with no redirects.
	tr := &http.Transport{Proxy: nil, DisableKeepAlives: true, MaxResponseHeaderBytes: 16 << 10, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext}
	client := &http.Client{Transport: tr, Jar: jar, Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return ErrUnsupported }}
	req, _ := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	kind, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if kind != "text/html" {
		return nil, ErrUnsupported
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, MaxPageBytes+1))
	if err != nil || len(b) > MaxPageBytes {
		return nil, ErrUnsupported
	}
	p, err := parse(u, string(b))
	if err != nil {
		return nil, err
	}
	p.client = client
	return p, nil
}
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}
func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(text(c))
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func parse(u *url.URL, body string) (*Page, error) {
	doc, err := html.Parse(strings.NewReader(body))
	if err != nil {
		return nil, ErrUnsupported
	}
	p := &Page{target: u, hidden: url.Values{}, View: api.WorkURLPage{Revision: rand.Text(), Origin: u.Scheme + "://" + u.Host, Fields: []api.WorkURLField{}, SubmitLabel: "Continue"}}
	var form *html.Node
	labels := map[string]string{}
	names := map[string]bool{}
	buttons := 0
	var scan func(*html.Node) error
	scan = func(n *html.Node) error {
		if n.Type == html.ElementNode {
			for _, a := range n.Attr {
				if strings.HasPrefix(a.Key, "on") || a.Key == "inert" {
					return ErrUnsupported
				}
			}
			switch n.Data {
			case "script", "iframe", "base", "object", "embed":
				return ErrUnsupported
			case "title":
				p.View.Title = text(n)
			case "label":
				if id := attr(n, "for"); id != "" {
					labels[id] = text(n)
				}
			case "form":
				if form != nil {
					return ErrUnsupported
				}
				form = n
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if e := scan(c); e != nil {
				return e
			}
		}
		return nil
	}
	if err = scan(doc); err != nil {
		return nil, err
	}

	var visible func(*html.Node) string
	visible = func(n *html.Node) string {
		if n.Type == html.ElementNode && (n.Data == "head" || n.Data == "style" || n.Data == "template") {
			return ""
		}
		if n.Type == html.TextNode {
			return n.Data
		}
		var b strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			b.WriteString(visible(c))
			b.WriteByte(' ')
		}
		return b.String()
	}
	p.View.Text = strings.Join(strings.Fields(visible(doc)), " ")
	if len(p.View.Title) > 512 || len(p.View.Text) > 8192 {
		return nil, ErrUnsupported
	}
	if form == nil || !strings.EqualFold(attr(form, "method"), "post") {
		return nil, ErrUnsupported
	}
	if enc := attr(form, "enctype"); enc != "" && enc != "application/x-www-form-urlencoded" {
		return nil, ErrUnsupported
	}
	action, err := u.Parse(attr(form, "action"))
	if err != nil || action.String() != u.String() {
		return nil, ErrUnsupported
	} // exact original path/query, not arbitrary same-origin endpoints
	var fields func(*html.Node) error
	fields = func(n *html.Node) error {
		if n.Type == html.ElementNode {
			// These native browser semantics are deliberately not projected. Inert
			// controls must not become successful fields, nor fixed values editable.
			if n.Data == "template" || n.Data == "fieldset" && hasAttr(n, "disabled") || hasAttr(n, "readonly") {
				return ErrUnsupported
			}
			if hasAttr(n, "form") || hasAttr(n, "formaction") || hasAttr(n, "formmethod") || hasAttr(n, "formenctype") {
				return ErrUnsupported
			}
			if n.Data == "input" || n.Data == "select" || n.Data == "textarea" || n.Data == "button" {
				if hasAttr(n, "disabled") {
					return ErrUnsupported
				}
				for _, constraint := range []string{"pattern", "min", "max", "step", "minlength", "maxlength", "multiple"} {
					if hasAttr(n, constraint) {
						return ErrUnsupported
					}
				}
				if n.Data == "textarea" {
					return ErrUnsupported
				}

				typ := strings.ToLower(attr(n, "type"))
				if n.Data == "button" && typ == "" {
					typ = "submit"
				}
				if n.Data == "input" && typ == "" {
					typ = "text"
				}
				name := attr(n, "name")
				if typ == "submit" {
					buttons++
					if buttons > 1 {
						return ErrUnsupported
					}
					if label := text(n); label != "" {
						p.View.SubmitLabel = label
					} else if v := attr(n, "value"); v != "" {
						p.View.SubmitLabel = v
					}
					if name != "" {
						if !namePattern.MatchString(name) || names[name] {
							return ErrUnsupported
						}
						names[name] = true
						p.hidden.Set(name, attr(n, "value"))
					}
					return nil
				}
				if !namePattern.MatchString(name) || names[name] || len(names) >= 32 {
					return ErrUnsupported
				}
				names[name] = true
				if typ == "hidden" {
					p.hidden.Set(name, attr(n, "value"))
					return nil
				}
				f := api.WorkURLField{Name: name, Label: labels[attr(n, "id")], Type: typ, Required: hasAttr(n, "required"), Value: attr(n, "value"), Options: []api.WorkURLOption{}}
				if f.Label == "" {
					for parent := n.Parent; parent != nil && parent != form; parent = parent.Parent {
						if parent.Type == html.ElementNode && parent.Data == "label" {
							f.Label = text(parent)
							break
						}
					}
				}
				if f.Label == "" {
					f.Label = attr(n, "aria-label")
				}
				if f.Label == "" {
					f.Label = name
				}
				if n.Data == "select" {
					if hasAttr(n, "multiple") {
						return ErrUnsupported
					}
					f.Type = "select"
					selected := false
					var options func(*html.Node) error
					options = func(node *html.Node) error {
						if node.Type == html.ElementNode && node.Data == "optgroup" && hasAttr(node, "disabled") {
							return ErrUnsupported
						}
						if node.Type == html.ElementNode && node.Data == "option" {
							if hasAttr(node, "disabled") {
								return ErrUnsupported
							}
							if !hasAttr(node, "value") {
								return ErrUnsupported
							} // no invented implicit option IDs
							v := attr(node, "value")
							f.Options = append(f.Options, api.WorkURLOption{Value: v, Label: text(node)})
							if hasAttr(node, "selected") {
								selected = true
								f.Value = v
							}
							if len(f.Options) > 32 {
								return ErrUnsupported
							}
						}
						for c := node.FirstChild; c != nil; c = c.NextSibling {
							if e := options(c); e != nil {
								return e
							}
						}
						return nil
					}
					if err := options(n); err != nil || len(f.Options) == 0 {
						return ErrUnsupported
					}
					if !selected {
						f.Value = f.Options[0].Value
					}
				}
				switch f.Type {
				case "text", "password", "email", "tel", "url", "search", "select":
				default:
					return ErrUnsupported
				}
				if f.Type == "password" {
					f.Value = ""
				}
				if len(f.Label) > 512 || len(f.Value) > 4096 {
					return ErrUnsupported
				}
				p.View.Fields = append(p.View.Fields, f)
				return nil
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if e := fields(c); e != nil {
				return e
			}
		}
		return nil
	}
	if err = fields(form); err != nil {
		return nil, err
	}
	if buttons != 1 {
		return nil, ErrUnsupported
	}
	return p, nil
}
func (p *Page) Encode(values map[string]string) (string, error) {
	if len(values) > len(p.View.Fields) {
		return "", ErrInvalid
	}
	out := url.Values{}
	for k, v := range p.hidden {
		out[k] = append([]string{}, v...)
	}
	allowed := map[string]bool{}
	for _, f := range p.View.Fields {
		allowed[f.Name] = true
		v := values[f.Name]
		if len(v) > 4096 || f.Required && v == "" {
			return "", ErrInvalid
		}
		if f.Type == "select" {
			found := false
			for _, o := range f.Options {
				if v == o.Value {
					found = true
				}
			}
			if !found {
				return "", ErrInvalid
			}
		}
		out.Set(f.Name, v)
	}
	for k := range values {
		if !allowed[k] {
			return "", ErrInvalid
		}
	}
	body := out.Encode()
	if len(body) > MaxPageBytes {
		return "", ErrInvalid
	}
	return body, nil
}
func (p *Page) Submit(ctx context.Context, body string) error {
	req, _ := http.NewRequestWithContext(ctx, "POST", p.target.String(), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", p.View.Origin)
	resp, err := p.client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	// A web receipt, never native approval/continuation. Discard response content.
	_, err = io.Copy(io.Discard, io.LimitReader(resp.Body, MaxPageBytes+1))
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ErrUnavailable
	}
	return nil
}
