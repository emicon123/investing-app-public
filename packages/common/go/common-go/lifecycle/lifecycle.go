// Package lifecycle owns the backend process lifecycle (ADR-110 Decision 1, D174 §2.2 and §4.1–§4.2):
//
//   - Group is the Supervisor of background workers: it gives them a context derived from the
//     signal context, cancels it on Stop, awaits them on Wait, and recovers a panic that escapes a
//     worker (backstop only).
//   - Guard is per-iteration panic recovery: every scheduler loop wraps one tick in it, so a panic
//     costs one tick, not the worker or the process (task-174 finding F1.3).
//   - Run is the Facade over listen → serve → ordered shutdown (stop workers, drain HTTP, await
//     workers) that all three main.go files used to hand-roll, never calling os.Exit (F1.2).
//   - Probe is the HTTP self-probe behind the `backend-go healthcheck` subcommand the compose
//     healthcheck executes inside the distroless image (no shell, no wget).
//
// Imports only the standard library.
package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

// Default shutdown budgets: 15 s drain + 10 s workers = 25 s, inside the compose
// stop_grace_period of 30 s.
const (
	DefaultDrainTimeout  = 15 * time.Second
	DefaultWorkerTimeout = 10 * time.Second
)

// Group owns background workers: it gives them a context derived from its parent, cancels it
// on Stop, and Wait blocks until every worker has returned.
type Group struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewGroup returns a Group whose worker context is derived from parent (the signal context).
func NewGroup(parent context.Context) *Group {
	ctx, cancel := context.WithCancel(parent)
	return &Group{ctx: ctx, cancel: cancel}
}

// Go starts fn(ctx) in a goroutine owned by g (sync.WaitGroup.Go). fn must return soon after
// ctx is done. A panic that escapes fn is recovered and logged at ERROR with the stack
// ("worker panicked; worker stopped", worker=name); the process keeps running. Loops should
// still call Guard per iteration so a panic costs one tick, not the whole worker.
func (g *Group) Go(name string, fn func(ctx context.Context)) {
	g.wg.Go(func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("worker panicked; worker stopped",
					"worker", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			}
		}()
		fn(g.ctx)
	})
}

// Stop cancels the workers' context. Idempotent.
func (g *Group) Stop() { g.cancel() }

// Wait blocks until all workers have returned or ctx is done; it returns ctx.Err() in the
// latter case (the caller logs which budget was exceeded and proceeds with shutdown).
func (g *Group) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		g.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Guard runs one unit of work. A panic inside fn is recovered, logged at ERROR with the stack
// (name, panic value), and returned as an error ("<name>: panic: <value>"); otherwise fn's
// own error is returned unchanged. It is what every scheduler loop wraps around one tick.
func Guard(name string, fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic recovered; unit of work aborted",
				"name", name, "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			err = fmt.Errorf("%s: panic: %v", name, r)
		}
	}()
	return fn()
}

// Options bounds the shutdown phases. Zero values mean the defaults
// (DefaultDrainTimeout, DefaultWorkerTimeout).
type Options struct {
	DrainTimeout  time.Duration // http.Server.Shutdown budget; default 15s
	WorkerTimeout time.Duration // Group.Wait budget after Stop; default 10s
}

func (o Options) withDefaults() Options {
	if o.DrainTimeout <= 0 {
		o.DrainTimeout = DefaultDrainTimeout
	}
	if o.WorkerTimeout <= 0 {
		o.WorkerTimeout = DefaultWorkerTimeout
	}
	return o
}

// Run listens on srv.Addr synchronously (a bind error is returned at once, before anything
// is served — the workers are stopped and awaited first so main's deferred pool.Close does
// not race them), logs "backend-go listening" (addr), serves until ctx is done or Serve fails,
// then shuts down in this order: (1) workers.Stop(); (2) srv.Shutdown with DrainTimeout
// (in-flight requests finish; their contexts are NOT cancelled by the signal);
// (3) workers.Wait with WorkerTimeout. An exceeded budget is logged at WARN and shutdown
// proceeds. It returns the Serve error (nil after a clean signal shutdown). It never calls
// os.Exit; main's deferred pool.Close runs after it returns.
func Run(ctx context.Context, srv *http.Server, workers *Group, opts Options) error {
	opts = opts.withDefaults()

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		workers.Stop()
		awaitWorkers(workers, opts.WorkerTimeout)
		return fmt.Errorf("lifecycle: listen on %s: %w", srv.Addr, err)
	}
	slog.Info("backend-go listening", "addr", srv.Addr)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	var result error
	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "error", err)
			result = err
		}
	}

	slog.Info("shutting down backend-go")
	workers.Stop()

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), opts.DrainTimeout)
	defer cancelDrain()
	if err := srv.Shutdown(drainCtx); err != nil {
		slog.Warn("lifecycle: shutdown budget exceeded; open HTTP connections dropped",
			"phase", "http-drain", "budget", opts.DrainTimeout.String(), "error", err)
	}

	awaitWorkers(workers, opts.WorkerTimeout)
	return result
}

func awaitWorkers(workers *Group, budget time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	if err := workers.Wait(ctx); err != nil {
		slog.Warn("lifecycle: shutdown budget exceeded; workers still running at exit",
			"phase", "workers", "budget", budget.String(), "error", err)
	}
}

// Probe performs GET url with ctx and returns nil iff the status is 2xx. Used by the
// `backend-go healthcheck` subcommand that the compose healthcheck executes inside the
// distroless image (no shell, no wget).
func Probe(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("lifecycle: probe %s: %w", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("lifecycle: probe %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }() // read-side close; error not actionable
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("lifecycle: probe %s: status %d", url, resp.StatusCode)
	}
	return nil
}
