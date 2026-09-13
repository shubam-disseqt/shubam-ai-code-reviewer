// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 disseqt

package docsserver

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"
)

// lockedBuf is a concurrency-safe buffer used to observe Start's stdout
// from the test goroutine without racing the server goroutine.
type lockedBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func TestStart_ServesEmbeddedAssets(t *testing.T) {
	t.Parallel()

	assets := fstest.MapFS{
		"index.html": &fstest.MapFile{
			Data: []byte("<html><body>hello docs</body></html>"),
		},
		"style.css": &fstest.MapFile{
			Data: []byte("body { color: red; }"),
		},
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &lockedBuf{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Start(ctx, Options{
			Addr:     addr,
			Assets:   assets,
			Stdout:   stdout,
			OpenMode: "never",
		})
	}()

	var resp *http.Response
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r, err := http.Get("http://" + addr + "/")
		if err == nil {
			resp = r
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if resp == nil {
		t.Fatalf("server never became ready; stdout=%q", stdout.String())
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET / status = %d; want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "hello docs") {
		t.Errorf("body = %q; expected embedded index content", string(body))
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q; want nosniff", got)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Start returned error on shutdown: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not return within 3s of context cancellation")
	}
}

func TestStart_RejectsMissingAssets(t *testing.T) {
	t.Parallel()
	err := Start(context.Background(), Options{Addr: "127.0.0.1:0", Stdout: io.Discard})
	if err == nil {
		t.Fatal("expected error when Assets is nil")
	}
}

func TestStart_DefaultAddrIsLoopback(t *testing.T) {
	t.Parallel()

	assets := fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stdout := &lockedBuf{}
	errCh := make(chan error, 1)
	go func() {
		errCh <- Start(ctx, Options{Assets: assets, Stdout: stdout, OpenMode: "never"})
	}()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(stdout.String(), "listening at") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	out := stdout.String()
	if !strings.Contains(out, "listening at http://127.0.0.1") &&
		!strings.Contains(out, "listening at http://localhost") {
		t.Errorf("expected loopback bind in stdout; got: %s", out)
	}
	cancel()
	<-errCh
}

func TestShouldOpen(t *testing.T) {
	t.Parallel()
	if shouldOpen("auto", &bytes.Buffer{}) {
		t.Error("shouldOpen(auto) with non-file writer should be false")
	}
	if !shouldOpen("always", &bytes.Buffer{}) {
		t.Error("shouldOpen(always) should be true regardless of writer")
	}
	if shouldOpen("never", os.Stdout) {
		t.Error("shouldOpen(never) should be false regardless of writer")
	}
}
