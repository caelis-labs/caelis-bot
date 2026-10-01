package nodeagent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/caelis-labs/caelis-bot/internal/nodeplane"
	"github.com/caelis-labs/caelis-bot/internal/workerwire"
)

const workerChunk = 128 << 10

// NativeWorkerProxyEndpoint is trusted native plan output. Paths and process launch
// parameters never occur in the agent Worker protocol.
type NativeWorkerProxyEndpoint struct {
	Pair   workerwire.Pair
	Socket string
}
type NativeWorkerProxyResolver func(context.Context, workerwire.Pair) (NativeWorkerProxyEndpoint, error)

// NativeWorkerProxy relays only approved paired Worker frames. Its owner context
// controls observers; the separately supervised native Worker retains its lifetime.
type NativeWorkerProxy struct {
	life     context.Context
	resolve  NativeWorkerProxyResolver
	mu       sync.Mutex
	sessions map[string]*workerProxySession
}
type workerProxySession struct {
	conn                    io.ReadWriteCloser
	pair                    workerwire.Pair
	mu                      sync.Mutex
	in                      []byte
	writeOffset, readOffset uint64
	frames                  int
	out                     []byte
	replies                 chan []byte
	done                    chan struct{}
	disposed                chan struct{}
	once                    sync.Once
	last                    time.Time
}
type workerProxyOpen struct {
	Pair workerwire.Pair `json:"pair"`
}
type workerProxyOpened struct {
	Handle string `json:"handle"`
}
type workerProxyIO struct {
	Handle string `json:"handle"`
	Offset uint64 `json:"offset"`
	Data   []byte `json:"data,omitempty"`
}
type workerProxyRead struct {
	Data   []byte `json:"data,omitempty"`
	Offset uint64 `json:"offset"`
	Closed bool   `json:"closed"`
}
type workerProxyClose struct {
	Handle string `json:"handle"`
}

// NewNativeWorkerProxy requires a native owner and trusted plan resolver. It
// never launches a Worker, adopts an arbitrary socket, or grants work admission.
func NewNativeWorkerProxy(owner context.Context, resolve NativeWorkerProxyResolver) *NativeWorkerProxy {
	if owner == nil || resolve == nil {
		return nil
	}
	p := &NativeWorkerProxy{life: owner, resolve: resolve, sessions: map[string]*workerProxySession{}}
	go func() {
		tick := time.NewTicker(time.Minute)
		defer tick.Stop()
		for {
			select {
			case <-owner.Done():
				p.mu.Lock()
				for h, s := range p.sessions {
					s.dispose()
					delete(p.sessions, h)
				}
				p.mu.Unlock()
				return
			case <-tick.C:
				p.mu.Lock()
				for h, s := range p.sessions {
					s.mu.Lock()
					idle := time.Since(s.last) > 5*time.Minute
					s.mu.Unlock()
					if idle {
						s.dispose()
						delete(p.sessions, h)
					}
				}
				p.mu.Unlock()
			}
		}
	}()
	return p
}
func (s *workerProxySession) close()   { s.once.Do(func() { close(s.done); _ = s.conn.Close() }) }
func (s *workerProxySession) dispose() { close(s.disposed); s.close() }
func (p *NativeWorkerProxy) open(ctx context.Context, pair workerwire.Pair) (workerProxyOpened, error) {
	if p == nil || p.resolve == nil || p.life.Err() != nil || workerwire.ValidateRelayPair(pair) != nil {
		return workerProxyOpened{}, errors.New("approved native Worker route unavailable")
	}
	ep, err := p.resolve(ctx, pair)
	if err != nil || ep.Pair != pair {
		return workerProxyOpened{}, errors.New("Worker route is not enrolled for this exact source")
	}
	conn, err := workerwire.DialRelayEndpoint(ctx, ep.Socket)
	if err != nil {
		return workerProxyOpened{}, err
	}
	var selector [24]byte
	if _, err = rand.Read(selector[:]); err != nil {
		_ = conn.Close()
		return workerProxyOpened{}, err
	}
	h := hex.EncodeToString(selector[:])
	s := &workerProxySession{conn: conn, pair: pair, replies: make(chan []byte, 1), done: make(chan struct{}), disposed: make(chan struct{}), last: time.Now()}
	p.mu.Lock()
	if len(p.sessions) >= 8 || p.life.Err() != nil {
		p.mu.Unlock()
		_ = conn.Close()
		return workerProxyOpened{}, errors.New("Worker observer limit")
	}
	p.sessions[h] = s
	p.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
		case <-s.disposed:
			return
		}
		p.close(h)
	}()
	go func() {
		defer s.close()
		for {
			b, e := workerwire.ReadRelayFrame(conn, pair, false)
			if e != nil {
				return
			}
			select {
			case s.replies <- b:
			case <-s.done:
				return
			case <-p.life.Done():
				return
			}
		}
	}()
	return workerProxyOpened{Handle: h}, nil
}
func (p *NativeWorkerProxy) session(h string) (*workerProxySession, error) {
	if p == nil || len(h) != 48 {
		return nil, errors.New("Worker observer unavailable")
	}
	p.mu.Lock()
	s := p.sessions[h]
	p.mu.Unlock()
	if s == nil {
		return nil, errors.New("Worker observer unavailable")
	}
	return s, nil
}
func (p *NativeWorkerProxy) write(v workerProxyIO) error {
	s, err := p.session(v.Handle)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
		return io.ErrClosedPipe
	default:
	}
	if len(v.Data) == 0 || len(v.Data) > workerChunk || v.Offset != s.writeOffset {
		s.close()
		return errors.New("Worker chunk is not the original ordered stream")
	}
	s.last = time.Now()
	s.writeOffset += uint64(len(v.Data))
	s.in = append(s.in, v.Data...)
	for len(s.in) >= 4 {
		n := binary.BigEndian.Uint32(s.in[:4])
		if n == 0 || n > workerwire.MaxRelayFrame {
			s.close()
			return errors.New("Worker frame limit")
		}
		if len(s.in) < int(n)+4 {
			return nil
		}
		frame, err := workerwire.ReadRelayFrame(bytes.NewReader(s.in[:int(n)+4]), s.pair, true)
		if err != nil || s.frames >= 8192 {
			s.close()
			return errors.New("closed Worker frame unavailable")
		}
		s.frames++
		if c, ok := s.conn.(net.Conn); ok {
			_ = c.SetWriteDeadline(time.Now().Add(3 * time.Second))
		}
		for len(frame) > 0 {
			written, e := s.conn.Write(frame)
			if e != nil || written <= 0 {
				s.close()
				return io.ErrClosedPipe
			}
			frame = frame[written:]
		}
		s.in = s.in[int(n)+4:]
	}
	return nil
}
func (p *NativeWorkerProxy) read(ctx context.Context, v workerProxyIO) (workerProxyRead, error) {
	s, err := p.session(v.Handle)
	if err != nil {
		return workerProxyRead{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(v.Data) != 0 || v.Offset != s.readOffset {
		s.close()
		return workerProxyRead{}, errors.New("Worker read offset mismatch")
	}
	s.last = time.Now()
	if len(s.out) == 0 {
		// A short poll bounds occupied agent slots and shared session locking,
		// leaving writes, catalog reads and lease verification available.
		timer := time.NewTimer(20 * time.Millisecond)
		defer timer.Stop()
		select {
		case s.out = <-s.replies:
		case <-s.done:
			// Preserve a final validated reply queued before native EOF.
			select {
			case s.out = <-s.replies:
			default:
				return workerProxyRead{Offset: s.readOffset, Closed: true}, nil
			}
		case <-ctx.Done():
			return workerProxyRead{}, ctx.Err()
		case <-timer.C:
			return workerProxyRead{Offset: s.readOffset}, nil
		}
	}
	n := min(len(s.out), workerChunk)
	b := append([]byte(nil), s.out[:n]...)
	s.out = s.out[n:]
	offset := s.readOffset
	s.readOffset += uint64(n)
	return workerProxyRead{Offset: offset, Data: b}, nil
}
func (p *NativeWorkerProxy) close(h string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	s := p.sessions[h]
	delete(p.sessions, h)
	p.mu.Unlock()
	if s != nil {
		s.dispose()
	}
}

func (s *Service) workerProxy() (*NativeWorkerProxy, string) {
	return s.options.WorkerProxy, s.options.NodeID
}

type workerProxyPort interface {
	workerProxy() (*NativeWorkerProxy, string)
}

func handleNativeWorkerProxy(agent nodeplane.CatalogAgent, w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/v1/node/worker/open" && r.URL.Path != "/v1/node/worker/write" && r.URL.Path != "/v1/node/worker/read" && r.URL.Path != "/v1/node/worker/close" {
		return false
	}
	port, ok := agent.(workerProxyPort)
	if !ok {
		http.Error(w, "native Worker route unavailable", 503)
		return true
	}
	p, node := port.workerProxy()
	if p == nil {
		http.Error(w, "native Worker route unavailable", 503)
		return true
	}
	var out any = struct{}{}
	var err error
	switch r.URL.Path {
	case "/v1/node/worker/open":
		var v workerProxyOpen
		if strictDecode(r.Body, &v) != nil || v.Pair.Target.NodeID != node {
			err = errors.New("invalid exact Worker target")
		} else {
			out, err = p.open(r.Context(), v.Pair)
		}
	case "/v1/node/worker/write":
		var v workerProxyIO
		if strictDecode(r.Body, &v) != nil {
			err = errors.New("invalid Worker chunk")
		} else {
			err = p.write(v)
		}
	case "/v1/node/worker/read":
		var v workerProxyIO
		if strictDecode(r.Body, &v) != nil {
			err = errors.New("invalid Worker read")
		} else {
			out, err = p.read(r.Context(), v)
		}
	case "/v1/node/worker/close":
		var v workerProxyClose
		if strictDecode(r.Body, &v) != nil {
			err = errors.New("invalid Worker close")
		} else {
			p.close(v.Handle)
		}
	}
	if err != nil {
		http.Error(w, "native Worker observer unavailable", 503)
	} else {
		_ = json.NewEncoder(w).Encode(out)
	}
	return true
}

type agentWorkerStream struct {
	client *Client
	handle string
	reader *io.PipeReader
	writer *io.PipeWriter
	life   context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	offset uint64
	once   sync.Once
}

// OpenWorkerStream attaches an observer to an already approved native Worker
// plan. The shared agent client and native process are not owned by this stream.
func (c *Client) OpenWorkerStream(ctx context.Context, pair workerwire.Pair) (io.ReadWriteCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pair.Target.NodeID != c.expected || workerwire.ValidateRelayPair(pair) != nil {
		return nil, errors.New("exact enrolled Worker target required")
	}
	var opened workerProxyOpened
	if err := c.request(context.WithoutCancel(ctx), "POST", "/v1/node/worker/open", workerProxyOpen{Pair: pair}, &opened); err != nil {
		return nil, err
	}
	if len(opened.Handle) != 48 {
		return nil, errors.New("invalid native Worker observer")
	}
	life, cancel := context.WithCancel(context.Background())
	r, w := io.Pipe()
	s := &agentWorkerStream{client: c, handle: opened.Handle, reader: r, writer: w, life: life, cancel: cancel}
	go s.read()
	return s, nil
}
func (s *agentWorkerStream) Read(b []byte) (int, error) { return s.reader.Read(b) }
func (s *agentWorkerStream) Write(b []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := 0
	for len(b) > 0 {
		if s.life.Err() != nil {
			return total, io.ErrClosedPipe
		}
		n := min(len(b), workerChunk)
		input := workerProxyIO{Handle: s.handle, Offset: s.offset, Data: b[:n]}
		var out struct{}
		if err := s.client.request(context.WithoutCancel(s.life), "POST", "/v1/node/worker/write", input, &out); err != nil {
			_ = s.Close()
			return total, err
		}
		s.offset += uint64(n)
		total += n
		b = b[n:]
	}
	return total, nil
}
func (s *agentWorkerStream) read() {
	defer s.Close()
	var offset uint64
	for {
		if s.life.Err() != nil {
			return
		}
		var out workerProxyRead
		err := s.client.request(context.WithoutCancel(s.life), "POST", "/v1/node/worker/read", workerProxyIO{Handle: s.handle, Offset: offset}, &out)
		if err != nil || out.Offset != offset || len(out.Data) > workerChunk || out.Closed {
			return
		}
		if len(out.Data) > 0 {
			if _, err = s.writer.Write(out.Data); err != nil {
				return
			}
			offset += uint64(len(out.Data))
		} else {
			select {
			case <-s.life.Done():
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
}
func (s *agentWorkerStream) Close() error {
	s.once.Do(func() {
		s.cancel()
		_ = s.reader.Close()
		_ = s.writer.Close()
		// Per-observer cancellation must not interrupt a shared agent frame halfway
		// through its write. The native stream owner handles a broken connection.
		go func() {
			var out struct{}
			_ = s.client.request(context.Background(), "POST", "/v1/node/worker/close", workerProxyClose{Handle: s.handle}, &out)
		}()
	})
	return nil
}
