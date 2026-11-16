package pgtx_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"investing-app/common-go/pgtx"
	"investing-app/common-go/testsupport"
)

// execer is the stand-in for a package's sqlc Queries interface: the narrow slice of the bound
// transaction a service actually calls.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// TransactFunc is declared exactly like every per-package TransactFunc in the apps; the runner's
// result must be assignable to it without conversion (D174 §2.1).
type TransactFunc func(ctx context.Context, fn func(execer) error) error

func bindExecer(tx pgx.Tx) execer { return tx }

// startPool starts one real Postgres (migrated by the real Flyway CLI image, never a mock) and
// creates the throwaway table every subtest writes to.
func startPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pg := testsupport.StartMigratedPostgres(t, "../testsupport/testdata/migrations")
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pg.ConnString)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `CREATE TABLE pgtx_probe (name TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create probe table: %v", err)
	}
	return pool
}

func rowExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pgtx_probe WHERE name = $1)`, name).Scan(&exists); err != nil {
		t.Fatalf("probe query: %v", err)
	}
	return exists
}

func insert(ctx context.Context, q execer, name string) error {
	_, err := q.Exec(ctx, `INSERT INTO pgtx_probe (name) VALUES ($1)`, name)
	return err
}

func TestRunner_RealPostgres(t *testing.T) {
	pool := startPool(t)
	var transact TransactFunc = pgtx.Runner(pool, bindExecer)
	ctx := context.Background()

	t.Run("commits when fn returns nil", func(t *testing.T) {
		if err := transact(ctx, func(q execer) error { return insert(ctx, q, "committed") }); err != nil {
			t.Fatalf("transact: %v", err)
		}
		if !rowExists(t, pool, "committed") {
			t.Fatal("row written inside a nil-returning fn is not committed")
		}
	})

	t.Run("rolls back and returns fn's error unchanged", func(t *testing.T) {
		boom := errors.New("boom")
		err := transact(ctx, func(q execer) error {
			if err := insert(ctx, q, "rolled-back"); err != nil {
				return err
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("expected fn's error, got %v", err)
		}
		if rowExists(t, pool, "rolled-back") {
			t.Fatal("row written before fn's error survived the rollback")
		}
	})

	t.Run("rolls back and releases the connection on panic", func(t *testing.T) {
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("expected the panic inside fn to propagate")
				}
			}()
			_ = transact(ctx, func(q execer) error {
				if err := insert(ctx, q, "panicked"); err != nil {
					return err
				}
				panic("boom inside the transaction")
			})
		}()
		if acquired := pool.Stat().AcquiredConns(); acquired != 0 {
			t.Fatalf("expected the panicking transaction's connection to be released, %d still acquired", acquired)
		}
		if rowExists(t, pool, "panicked") {
			t.Fatal("row written before the panic survived")
		}
	})
}

// bindTx exposes the whole transaction to fn: the serialisation tests need QueryRow and Exec.
func bindTx(tx pgx.Tx) pgx.Tx { return tx }

// incrementOnce is the read-then-write shape every money service has (read units, compute the
// new absolute value in Go, write it back) — exactly the lost-update pattern of F3.1.
func incrementOnce(ctx context.Context, tx pgx.Tx) error {
	var n int
	if err := tx.QueryRow(ctx, `SELECT n FROM pgtx_counter WHERE id = 1`).Scan(&n); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE pgtx_counter SET n = $1 WHERE id = 1`, n+1)
	return err
}

func TestSerializedRunner_RealPostgres(t *testing.T) {
	pool := startPool(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `CREATE TABLE pgtx_counter (id INT PRIMARY KEY, n INT NOT NULL);
		INSERT INTO pgtx_counter (id, n) VALUES (1, 0)`); err != nil {
		t.Fatalf("create counter table: %v", err)
	}
	transact := pgtx.SerializedRunner(pool, pgtx.MoneyWriteLockKey, bindTx)

	t.Run("concurrent read-then-write increments never lose an update", func(t *testing.T) {
		const workers, perWorker = 2, 50
		errs := make(chan error, workers*perWorker)
		var wg sync.WaitGroup
		for range workers {
			wg.Go(func() {
				for range perWorker {
					errs <- transact(ctx, func(tx pgx.Tx) error { return incrementOnce(ctx, tx) })
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("serialised increment: %v", err)
			}
		}
		var n int
		if err := pool.QueryRow(ctx, `SELECT n FROM pgtx_counter WHERE id = 1`).Scan(&n); err != nil {
			t.Fatalf("read counter: %v", err)
		}
		if n != workers*perWorker {
			t.Fatalf("counter = %d, want %d (a serialised read-then-write lost an update)", n, workers*perWorker)
		}
	})

	t.Run("a waiting transaction gives up when its ctx expires and releases its connection", func(t *testing.T) {
		holding := make(chan struct{})
		release := make(chan struct{})
		var releaseOnce sync.Once
		releaseHolder := func() { releaseOnce.Do(func() { close(release) }) }
		defer releaseHolder() // a failed assertion must never leave the holder blocking pool.Close
		holderDone := make(chan error, 1)
		go func() {
			holderDone <- transact(ctx, func(pgx.Tx) error {
				close(holding)
				<-release
				return nil
			})
		}()
		<-holding

		waitCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		ran := false
		err := transact(waitCtx, func(pgx.Tx) error { ran = true; return nil })
		if err == nil {
			t.Fatal("expected the waiter to fail while the lock is held")
		}
		if ran {
			t.Fatal("the waiter's fn ran without the lock")
		}
		if !errors.Is(err, context.DeadlineExceeded) && !pgconn.Timeout(err) {
			t.Fatalf("expected a context-deadline error, got %v", err)
		}
		// pgxpool destroys the waiter's broken connection asynchronously; only the holder's
		// connection may stay acquired.
		waitForAcquired(t, pool, 1)

		releaseHolder()
		if err := <-holderDone; err != nil {
			t.Fatalf("holder: %v", err)
		}
		// No deadlock and no lock left behind by the cancelled waiter: a fresh serialised
		// transaction gets the lock at once.
		nextCtx, cancelNext := context.WithTimeout(ctx, 5*time.Second)
		defer cancelNext()
		if err := transact(nextCtx, func(pgx.Tx) error { return nil }); err != nil {
			t.Fatalf("serialised transaction after the timed-out waiter: %v", err)
		}
		waitForAcquired(t, pool, 0)
	})
}

// waitForAcquired polls the pool until exactly want connections are acquired (a broken
// connection is destroyed in the background), failing after 20 s.
func waitForAcquired(t *testing.T, pool *pgxpool.Pool, want int32) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		got := pool.Stat().AcquiredConns()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected %d acquired connections, still %d after 20s", want, got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
