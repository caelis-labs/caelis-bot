package wirejson

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
)

// Bot consumes messages and lifecycle facts, not native tool output. Project at
// the byte boundary so screenshots and tool payloads never enter its event queue
// or transcript. The native Runtime remains the owner of the original bytes.
var ErrTooLarge = errors.New("projected JSON too large")
var ErrInvalid = errors.New("invalid JSON")

const maxWireFrame = 8 * 1024 * 1024

type Header struct {
	ID     json.RawMessage
	Method string
}

type wireReader struct {
	r        *bufio.Reader
	out      bytes.Buffer
	overflow bool
	header   Header
	path     []string
	opaque   bool
	line     bool
	started  bool
}

func Read(r *bufio.Reader) ([]byte, Header, error) {
	return read(r, false)
}
func ReadLine(r *bufio.Reader) ([]byte, Header, error) { return read(r, true) }
func read(r *bufio.Reader, line bool) ([]byte, Header, error) {
	p := &wireReader{r: r, line: line}
	if err := p.value(true, false, 0); err != nil {
		return nil, p.header, err
	}
	if p.overflow {
		return nil, p.header, ErrTooLarge
	}
	if !json.Valid(p.out.Bytes()) {
		return nil, p.header, ErrInvalid
	}
	return p.out.Bytes(), p.header, nil
}

func (p *wireReader) write(b byte, keep bool) {
	if !keep || p.overflow {
		return
	}
	if p.out.Len() >= maxWireFrame {
		p.overflow = true
		return
	}
	p.out.WriteByte(b)
}
func (p *wireReader) next() (byte, error) {
	for {
		b, err := p.r.ReadByte()
		if err != nil {
			return 0, err
		}
		if p.line && p.started && (b == '\n' || b == '\r') {
			return 0, ErrInvalid
		}
		if b != ' ' && b != '\n' && b != '\r' && b != '\t' {
			p.started = true
			return b, nil
		}
	}
}
func (p *wireReader) string(first byte, keep, key bool) ([]byte, error) {
	p.write(first, keep)
	var raw []byte
	if key {
		raw = append(raw, first)
	}
	escaped := false
	for {
		b, err := p.r.ReadByte()
		if err != nil {
			return nil, err
		}
		p.write(b, keep)
		if key {
			if len(raw) > 4096 {
				return nil, ErrInvalid
			}
			raw = append(raw, b)
		}
		if b == '"' && !escaped {
			return raw, nil
		}
		if b < 0x20 {
			return nil, ErrInvalid
		}
		if escaped {
			escaped = false
		} else {
			escaped = b == '\\'
		}
	}
}
func (p *wireReader) value(keep, item bool, depth int) error {
	if depth > 256 {
		return ErrInvalid
	}
	b, err := p.next()
	if err != nil {
		return err
	}
	p.write(b, keep)
	switch b {
	case '"':
		// string writes the opening quote itself.
		if keep && !p.overflow {
			p.out.Truncate(p.out.Len() - 1)
		}
		_, err = p.string(b, keep, false)
		return err
	case '{':
		b, err = p.next()
		if err != nil {
			return err
		}
		if b == '}' {
			p.write(b, keep)
			return nil
		}
		_ = p.r.UnreadByte()
		written := false
		for {
			b, err = p.next()
			if err != nil {
				return err
			}
			if b != '"' {
				return ErrInvalid
			}
			raw, err := p.string(b, false, true)
			if err != nil {
				return err
			}
			var name string
			if json.Unmarshal(raw, &name) != nil {
				return ErrInvalid
			}
			b, err = p.next()
			if err != nil {
				return err
			}
			if b != ':' {
				return ErrInvalid
			}
			toolUpdate := slices.Contains(p.path, "update")
			retain := keep
			if !p.opaque {
				if slices.Contains(p.path, "content") && (name == "data" || name == "base64" || item && name == "url") {
					retain = false
				}
				if item && (name == "arguments" || name == "result" || name == "aggregatedOutput") {
					retain = false
				}
				if toolUpdate && (name == "rawInput" || name == "rawOutput") {
					// Selected fields below retain command/resource evidence, not bytes.
				} else if toolUpdate && len(p.path) > 0 && p.path[len(p.path)-1] == "rawInput" && name != "action" {
					retain = false
				} else if toolUpdate && len(p.path) > 0 && p.path[len(p.path)-1] == "rawOutput" && !slices.Contains([]string{"handle", "state", "tasks", "resource", "result"}, name) {
					retain = false
				}
				if toolUpdate && name == "content" {
					b, err = p.next()
					if err != nil {
						return err
					}
					_ = p.r.UnreadByte()
					if b == '[' {
						retain = false
					}
				}
			}

			if retain {
				if written {
					p.write(',', true)
				}
				written = true
				for _, c := range raw {
					p.write(c, true)
				}
				p.write(':', true)
			}
			if depth == 0 && (name == "id" || name == "method") {
				header := &wireReader{r: p.r, line: p.line, started: true}
				if err = header.value(true, false, depth+1); err != nil {
					return err
				}
				if header.overflow {
					return ErrInvalid
				}
				raw := header.out.Bytes()
				if name == "id" {
					p.header.ID = append(json.RawMessage(nil), raw...)
				}
				if name == "method" {
					_ = json.Unmarshal(raw, &p.header.Method)
				}
				for _, c := range raw {
					p.write(c, retain)
				}
			} else {
				if retain && !p.opaque && toolUpdate && name == "result" && len(p.path) > 0 && p.path[len(p.path)-1] == "rawOutput" {
					prefix, peekErr := p.next()
					if peekErr != nil {
						return peekErr
					}
					_ = p.r.UnreadByte()
					if prefix == '"' {
						if err = p.optionalString(retain, 256<<10); err != nil {
							return err
						}
						b, err = p.next()
						if err != nil {
							return err
						}
						if b == '}' {
							p.write(b, keep)
							return nil
						}
						if b != ',' {
							return ErrInvalid
						}
						continue
					}
				}
				previousOpaque := p.opaque
				p.opaque = p.opaque || slices.Contains([]string{"requestedSchema", "permissions", "availableDecisions", "questions", "_meta"}, name)
				p.path = append(p.path, name)
				err = p.value(retain, item || !p.opaque && (name == "item" || name == "items"), depth+1)
				p.path = p.path[:len(p.path)-1]
				p.opaque = previousOpaque
				if err != nil {
					return err
				}
			}

			b, err = p.next()
			if err != nil {
				return err
			}
			if b == '}' {
				p.write(b, keep)
				return nil
			}
			if b != ',' {
				return ErrInvalid
			}
		}
	case '[':
		b, err = p.next()
		if err != nil {
			return err
		}
		if b == ']' {
			p.write(b, keep)
			return nil
		}
		_ = p.r.UnreadByte()
		for {
			if err = p.value(keep, item, depth+1); err != nil {
				return err
			}
			b, err = p.next()
			if err != nil {
				return err
			}
			p.write(b, keep)
			if b == ']' {
				return nil
			}
			if b != ',' {
				return ErrInvalid
			}
		}
	default:
		if !bytes.ContainsRune([]byte("-0123456789tfn"), rune(b)) {
			return ErrInvalid
		}
		for {
			b, err = p.r.ReadByte()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if b == ',' || b == ']' || b == '}' || b == '\n' || b == '\r' || b == ' ' || b == '\t' {
				_ = p.r.UnreadByte()
				return nil
			}
			p.write(b, keep)
		}
	}
}

// Small native resource receipts remain intact. A large optional tool result
// string is drained with bounded scratch space and replaced by null.
func (p *wireReader) optionalString(keep bool, limit int) error {
	var raw []byte
	escaped, large := false, false
	for n := 0; ; n++ {
		b, err := p.r.ReadByte()
		if err != nil {
			return err
		}
		if !large {
			if len(raw) >= limit {
				large = true
				raw = nil
			} else {
				raw = append(raw, b)
			}
		}
		if n > 0 && b == '"' && !escaped {
			break
		}
		if b < 0x20 {
			return ErrInvalid
		}
		if escaped {
			escaped = false
		} else if n > 0 {
			escaped = b == '\\'
		}
	}
	if large {
		raw = []byte("null")
	}
	for _, b := range raw {
		p.write(b, keep)
	}
	return nil
}
