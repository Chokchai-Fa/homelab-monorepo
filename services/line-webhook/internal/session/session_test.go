package session

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// fakeRedisServer is a minimal in-process RESP2 server implementing just
// enough of the Redis wire protocol (SET/DEL/EXISTS/EXPIRE plus the
// handshake commands go-redis sends on connect) to exercise this package's
// Redis-backed logic without a real Redis instance or network access beyond
// loopback.
type fakeRedisServer struct {
	mu   sync.Mutex
	data map[string]string
	ln   net.Listener
}

func newFakeRedisServer(t *testing.T) *fakeRedisServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	s := &fakeRedisServer{data: map[string]string{}, ln: ln}
	go s.serve()
	t.Cleanup(func() { ln.Close() })
	return s
}

func (s *fakeRedisServer) addr() string { return s.ln.Addr().String() }

func (s *fakeRedisServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *fakeRedisServer) handleConn(conn net.Conn) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	for {
		args, err := readRESPCommand(r)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		resp := s.dispatch(args)
		if _, err := conn.Write(resp); err != nil {
			return
		}
	}
}

func (s *fakeRedisServer) dispatch(args []string) []byte {
	cmd := strings.ToUpper(args[0])
	switch cmd {
	case "HELLO":
		// Signal "unsupported" so go-redis falls back to RESP2.
		return respError("ERR unknown command 'HELLO'")
	case "PING":
		return respSimpleString("PONG")
	case "SELECT", "AUTH", "CLIENT", "READONLY":
		return respSimpleString("OK")
	case "SET":
		return s.handleSet(args[1:])
	case "DEL":
		return s.handleDel(args[1:])
	case "EXISTS":
		return s.handleExists(args[1:])
	case "EXPIRE":
		return s.handleExpire(args[1:])
	default:
		return respError("ERR unknown command '" + cmd + "'")
	}
}

func (s *fakeRedisServer) handleSet(args []string) []byte {
	if len(args) < 2 {
		return respError("ERR wrong number of arguments for 'set' command")
	}
	key, value := args[0], args[1]
	nx := false
	for _, a := range args[2:] {
		if strings.EqualFold(a, "NX") {
			nx = true
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if nx {
		if _, exists := s.data[key]; exists {
			return respNilBulk()
		}
	}
	s.data[key] = value
	return respSimpleString("OK")
}

func (s *fakeRedisServer) handleDel(args []string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range args {
		if _, ok := s.data[k]; ok {
			delete(s.data, k)
			n++
		}
	}
	return respInteger(n)
}

func (s *fakeRedisServer) handleExists(args []string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, k := range args {
		if _, ok := s.data[k]; ok {
			n++
		}
	}
	return respInteger(n)
}

func (s *fakeRedisServer) handleExpire(args []string) []byte {
	if len(args) < 1 {
		return respError("ERR wrong number of arguments for 'expire' command")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.data[args[0]]; !ok {
		return respInteger(0)
	}
	return respInteger(1)
}

func readRESPCommand(r *bufio.Reader) ([]string, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	if len(line) == 0 || line[0] != '*' {
		return nil, fmt.Errorf("fakeredis: expected array, got %q", line)
	}
	n, err := strconv.Atoi(line[1:])
	if err != nil {
		return nil, err
	}
	args := make([]string, 0, n)
	for i := 0; i < n; i++ {
		typeLine, err := readLine(r)
		if err != nil {
			return nil, err
		}
		if len(typeLine) == 0 || typeLine[0] != '$' {
			return nil, fmt.Errorf("fakeredis: expected bulk string, got %q", typeLine)
		}
		l, err := strconv.Atoi(typeLine[1:])
		if err != nil {
			return nil, err
		}
		buf := make([]byte, l+2) // +2 for trailing \r\n
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		args = append(args, string(buf[:l]))
	}
	return args, nil
}

func readLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

func respSimpleString(s string) []byte { return []byte("+" + s + "\r\n") }
func respError(s string) []byte        { return []byte("-" + s + "\r\n") }
func respInteger(n int) []byte         { return []byte(":" + strconv.Itoa(n) + "\r\n") }
func respNilBulk() []byte              { return []byte("$-1\r\n") }

func newTestClient(t *testing.T) *redis.Client {
	t.Helper()
	srv := newFakeRedisServer(t)
	rdb := redis.NewClient(&redis.Options{Addr: srv.addr()})
	t.Cleanup(func() { rdb.Close() })
	return rdb
}

func TestKeyFormats(t *testing.T) {
	if got, want := key("u1"), "chat:ai_session:u1"; got != want {
		t.Errorf("key() = %q, want %q", got, want)
	}
	if got, want := flowKey("u1"), "chat:reminder_flow:u1"; got != want {
		t.Errorf("flowKey() = %q, want %q", got, want)
	}
}

func TestStartAndActive(t *testing.T) {
	rdb := newTestClient(t)
	s := New(rdb, time.Minute)
	ctx := context.Background()

	// No session yet.
	if s.Active(ctx, "u1") {
		t.Fatal("Active() before Start() = true, want false")
	}

	if err := s.Start(ctx, "u1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !s.Active(ctx, "u1") {
		t.Fatal("Active() after Start() = false, want true")
	}

	// A different user must not be affected.
	if s.Active(ctx, "u2") {
		t.Fatal("Active() for a different user = true, want false")
	}
}

func TestEnd(t *testing.T) {
	rdb := newTestClient(t)
	s := New(rdb, time.Minute)
	ctx := context.Background()

	if err := s.Start(ctx, "u1"); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := s.End(ctx, "u1"); err != nil {
		t.Fatalf("End() error = %v", err)
	}
	if s.Active(ctx, "u1") {
		t.Fatal("Active() after End() = true, want false")
	}
}

func TestFlowActive(t *testing.T) {
	rdb := newTestClient(t)
	s := New(rdb, time.Minute)
	ctx := context.Background()

	if s.FlowActive(ctx, "u1") {
		t.Fatal("FlowActive() with no flow key = true, want false")
	}

	if err := rdb.Set(ctx, flowKey("u1"), "1", time.Minute).Err(); err != nil {
		t.Fatalf("seed set error = %v", err)
	}
	if !s.FlowActive(ctx, "u1") {
		t.Fatal("FlowActive() with flow key present = false, want true")
	}

	// An AI session key must not be mistaken for a reminder flow.
	if s.FlowActive(ctx, "u2") {
		t.Fatal("FlowActive() for unrelated user = true, want false")
	}
}

func TestActiveAndFlowActiveDegradeOnRedisError(t *testing.T) {
	// Point at a port nothing is listening on so Redis commands fail;
	// both methods must degrade to "inactive" rather than panic or block,
	// keeping the webhook usable with a broken Redis (the /ai prefix alone
	// still works, per the doc comment on Store.Active).
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 200 * time.Millisecond,
	})
	defer rdb.Close()
	s := New(rdb, time.Minute)
	ctx := context.Background()

	if s.Active(ctx, "u1") {
		t.Fatal("Active() with unreachable redis = true, want false")
	}
	if s.FlowActive(ctx, "u1") {
		t.Fatal("FlowActive() with unreachable redis = true, want false")
	}
}
