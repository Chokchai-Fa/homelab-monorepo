package profilegate

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
// enough of the Redis wire protocol (SET/DEL plus the handshake commands
// go-redis sends on connect) to exercise this package's Redis-backed logic
// without a real Redis instance or network access beyond loopback.
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

func TestTryClaim(t *testing.T) {
	rdb := newTestClient(t)
	g := New(rdb, time.Minute)
	ctx := context.Background()

	// First caller wins the claim.
	if !g.TryClaim(ctx, "u1") {
		t.Fatal("first TryClaim() = false, want true")
	}
	// A second caller within the TTL must not also win it (dedup).
	if g.TryClaim(ctx, "u1") {
		t.Fatal("second TryClaim() = true, want false (already claimed)")
	}
	// A different user is independent.
	if !g.TryClaim(ctx, "u2") {
		t.Fatal("TryClaim() for a different user = false, want true")
	}
}

func TestReleaseAllowsRetry(t *testing.T) {
	rdb := newTestClient(t)
	g := New(rdb, time.Minute)
	ctx := context.Background()

	if !g.TryClaim(ctx, "u1") {
		t.Fatal("TryClaim() = false, want true")
	}
	g.Release(ctx, "u1")
	if !g.TryClaim(ctx, "u1") {
		t.Fatal("TryClaim() after Release() = false, want true")
	}
}

func TestTryClaimDegradesOnRedisError(t *testing.T) {
	// Point at a port nothing is listening on so the Redis command fails;
	// TryClaim must degrade to "no" rather than panic or block forever, so
	// a broken Redis never triggers a GetProfile stampede.
	rdb := redis.NewClient(&redis.Options{
		Addr:        "127.0.0.1:1",
		DialTimeout: 200 * time.Millisecond,
	})
	defer rdb.Close()
	g := New(rdb, time.Minute)

	if g.TryClaim(context.Background(), "u1") {
		t.Fatal("TryClaim() with unreachable redis = true, want false")
	}
}

func TestKeyFormat(t *testing.T) {
	if got, want := key("u1"), "chat:profile_seen:u1"; got != want {
		t.Errorf("key() = %q, want %q", got, want)
	}
}
