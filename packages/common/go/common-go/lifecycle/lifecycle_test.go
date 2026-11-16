package lifecycle

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// syncBuffer is a goroutine-safe log sink (workers log from their own goroutines).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// captureLogs routes slog's default logger into a buffer for the rest of the test.
func captureLogs(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	return buf
}

func TestGroup_StopThenWaitReturnsAfterWorkersExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := NewGroup(context.Background())
		var exited atomic.Int32
		for range 3 {
			g.Go("ticker", func(ctx context.Context) {
				ticker := time.NewTicker(time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						time.Sleep(500 * time.Millisecond) // a worker finishing its tick
						exited.Add(1)
						return
					case <-ticker.C:
					}
				}
			})
		}
		time.Sleep(5 * time.Second)

		g.Stop()
		g.Stop() // idempotent
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := g.Wait(ctx); err != nil {
			t.Fatalf("Wait: %v", err)
		}
		if got := exited.Load(); got != 3 {
			t.Fatalf("Wait returned before every worker exited: %d of 3", got)
		}
	})
}

func TestGroup_WorkersSeeParentCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		parent, cancelParent := context.WithCancel(context.Background())
		g := NewGroup(parent)
		g.Go("child", func(ctx context.Context) { <-ctx.Done() })
		cancelParent()
		if err := g.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
	})
}

func TestGroup_WaitReturnsDeadlineExceededWhenAWorkerIgnoresCtx(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := NewGroup(context.Background())
		release := make(chan struct{})
		g.Go("stubborn", func(context.Context) { <-release }) // ignores ctx
		g.Stop()
		defer func() {
			// Let the stubborn worker (and Wait's helper goroutine) exit before the bubble ends.
			close(release)
			_ = g.Wait(context.Background())
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		start := time.Now()
		err := g.Wait(ctx)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected context.DeadlineExceeded, got %v", err)
		}
		if waited := time.Since(start); waited != 10*time.Second {
			t.Fatalf("expected Wait to give up at the 10s budget, waited %v", waited)
		}
	})
}

func TestGroup_PanickingWorkerIsLoggedAndDoesNotCrash(t *testing.T) {
	logs := captureLogs(t)
	synctest.Test(t, func(t *testing.T) {
		g := NewGroup(context.Background())
		g.Go("exploding", func(context.Context) { panic("kaboom") })
		var survivorRan atomic.Bool
		g.Go("survivor", func(ctx context.Context) {
			<-ctx.Done()
			survivorRan.Store(true)
		})
		time.Sleep(time.Second)
		g.Stop()
		if err := g.Wait(context.Background()); err != nil {
			t.Fatalf("Wait: %v", err)
		}
		if !survivorRan.Load() {
			t.Fatal("a sibling worker did not keep running after another worker panicked")
		}
	})
	out := logs.String()
	for _, want := range []string{"worker panicked; worker stopped", "worker=exploding", "kaboom", "level=ERROR"} {
		if !strings.Contains(out, want) {
			t.Errorf("panic log missing %q:\n%s", want, out)
		}
	}
}

func TestGuard(t *testing.T) {
	logs := captureLogs(t)

	t.Run("nil passes through", func(t *testing.T) {
		if err := Guard("tick", func() error { return nil }); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})

	t.Run("fn's error is returned unchanged", func(t *testing.T) {
		boom := errors.New("boom")
		if err := Guard("tick", func() error { return boom }); err != boom { //nolint:errorlint // identity (not errors.Is) is what "returned unchanged" asserts
			t.Fatalf("expected the identical error value, got %v", err)
		}
	})

	t.Run("panic becomes an error naming the unit of work", func(t *testing.T) {
		err := Guard("price-refresh", func() error { panic("nil map write") })
		if err == nil {
			t.Fatal("expected the panic to be returned as an error")
		}
		if msg := err.Error(); !strings.Contains(msg, "price-refresh") || !strings.Contains(msg, "panic") || !strings.Contains(msg, "nil map write") {
			t.Fatalf("error should name the work and the panic value, got %q", msg)
		}
		out := logs.String()
		if !strings.Contains(out, "level=ERROR") || !strings.Contains(out, "price-refresh") || !strings.Contains(out, "stack=") {
			t.Fatalf("panic should be logged at ERROR with name and stack:\n%s", out)
		}
	})
}

func TestProbe(t *testing.T) {
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer ok.Close()
	unavailable := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unavailable.Close()
	refused := closedAddr(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := Probe(ctx, ok.URL+"/api/health-go"); err != nil {
		t.Errorf("200: expected nil, got %v", err)
	}
	if err := Probe(ctx, unavailable.URL+"/api/health-go"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Errorf("503: expected an error naming the status, got %v", err)
	}
	if err := Probe(ctx, "http://"+refused+"/api/health-go"); err == nil {
		t.Error("connection refused: expected an error, got nil")
	}
}

func TestRun_ReturnsBindErrorImmediately(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = taken.Close() }()

	srv := &http.Server{Addr: taken.Addr().String(), Handler: http.NotFoundHandler()}
	done := make(chan error, 1)
	go func() { done <- Run(context.Background(), srv, NewGroup(context.Background()), Options{}) }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected a bind error, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return the bind error at once")
	}
}

func TestRun_OnCancelStopsWorkersThenDrainsHTTPThenWaits(t *testing.T) {
	logs := captureLogs(t)
	addr := closedAddr(t)

	// In-flight request: still running when the signal arrives, must finish with its own
	// (uncancelled) context during the drain.
	requestStarted := make(chan struct{})
	var requestCtxCancelled atomic.Bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			close(requestStarted)
			time.Sleep(300 * time.Millisecond)
			requestCtxCancelled.Store(r.Context().Err() != nil)
		}
		w.WriteHeader(http.StatusOK)
	})
	srv := &http.Server{Addr: addr, Handler: handler}

	// Workers live on a context of their own, so only Run's Stop can cancel them.
	workers := NewGroup(context.Background())
	workerCtx := make(chan context.Context, 1)
	shutdownCalled := make(chan struct{})
	var workerReturned atomic.Bool
	workers.Go("spy", func(ctx context.Context) {
		workerCtx <- ctx
		<-ctx.Done()
		<-shutdownCalled // returns only once Shutdown has begun
		workerReturned.Store(true)
	})
	wctx := <-workerCtx

	var stoppedBeforeShutdown atomic.Bool
	srv.RegisterOnShutdown(func() {
		stoppedBeforeShutdown.Store(wctx.Err() != nil)
		close(shutdownCalled)
	})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, srv, workers, Options{DrainTimeout: 5 * time.Second, WorkerTimeout: 2 * time.Second})
	}()
	waitUntilServing(t, "http://"+addr+"/")

	slowStatus := make(chan int, 1)
	go func() {
		resp, err := http.Get("http://" + addr + "/slow")
		if err != nil {
			slowStatus <- -1
			return
		}
		_ = resp.Body.Close()
		slowStatus <- resp.StatusCode
	}()
	<-requestStarted

	start := time.Now()
	cancel()
	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after ctx was cancelled")
	}
	elapsed := time.Since(start)

	if runErr != nil {
		t.Fatalf("expected nil after a clean signal shutdown, got %v", runErr)
	}
	if !stoppedBeforeShutdown.Load() {
		t.Error("workers were not stopped before the HTTP server began shutting down")
	}
	if !workerReturned.Load() {
		t.Error("Run returned before the workers had returned (Wait must follow Shutdown)")
	}
	if elapsed >= 2*time.Second {
		t.Errorf("Run took %v: Wait ran before Shutdown and hit the worker budget", elapsed)
	}
	if status := <-slowStatus; status != http.StatusOK {
		t.Errorf("in-flight request should complete during the drain, got status %d", status)
	}
	if requestCtxCancelled.Load() {
		t.Error("the in-flight request's context was cancelled by the shutdown signal")
	}
	out := logs.String()
	for _, want := range []string{"backend-go listening", "shutting down backend-go"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q:\n%s", want, out)
		}
	}
}

func TestRun_WarnsWhenTheWorkerBudgetIsExceeded(t *testing.T) {
	logs := captureLogs(t)
	srv := &http.Server{Addr: closedAddr(t), Handler: http.NotFoundHandler()}
	workers := NewGroup(context.Background())
	release := make(chan struct{})
	defer close(release)
	workers.Go("stubborn", func(context.Context) { <-release })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Run(ctx, srv, workers, Options{WorkerTimeout: 50 * time.Millisecond}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	out := logs.String()
	if !strings.Contains(out, "level=WARN") || !strings.Contains(out, "budget=50ms") {
		t.Fatalf("expected a WARN naming the exceeded worker budget:\n%s", out)
	}
}

// closedAddr returns a 127.0.0.1 address nothing listens on (bound once, then released).
func closedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func waitUntilServing(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(url); err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server at %s never started serving", url)
}
