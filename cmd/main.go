package main

import (
	"context"
	"database/sql"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"b-log.com/b-log-worker/internal/infra"
	"b-log.com/b-log-worker/internal/worker"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/nats-io/nats.go"
)

const (
	defaultJSDomain    = "prod"
	defaultPingInt     = 20 * time.Second
	defaultReconnect   = 2 * time.Second
	defaultConnTimeout = 5 * time.Second
	maxWait            = 5 * time.Second
	dbConnMaxLife      = 30 * time.Minute
	dbMaxIdleTime      = 5 * time.Minute
	dbMaxOpenConns     = 10
	dbMaxIdleConns     = 5
)

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	time.Local = time.UTC

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("missing required env var: DATABASE_URL")
	}
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		log.Fatal("missing required env var: NATS_URL")
	}
	jsDomain := getenv("JS_DOMAIN", defaultJSDomain)

	home, _ := os.UserHomeDir()
	sharedDir := getenv("SHARED_DIR", filepath.Join(home, "b-log-uploads"))
	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		log.Fatalf("ensure shared dir error: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		log.Fatalf("db open error: %v", err)
	}
	defer db.Close()
	db.SetConnMaxLifetime(dbConnMaxLife)
	db.SetConnMaxIdleTime(dbMaxIdleTime)
	db.SetMaxOpenConns(dbMaxOpenConns)
	db.SetMaxIdleConns(dbMaxIdleConns)

	{
		pctx, c := context.WithTimeout(ctx, defaultConnTimeout)
		defer c()
		if err := db.PingContext(pctx); err != nil {
			log.Fatalf("db ping error: %v", err)
		}
	}

	proc, err := worker.New(db, sharedDir)
	if err != nil {
		log.Fatalf("worker init error: %v", err)
	}

	nc, err := nats.Connect(
		natsURL,
		nats.Name("b-log md-worker"),
		nats.Timeout(defaultConnTimeout),
		nats.PingInterval(defaultPingInt),
		nats.MaxPingsOutstanding(3),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(defaultReconnect),
		nats.RetryOnFailedConnect(true),
		nats.NoEcho(),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			log.Printf("nats disconnect: %v", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			log.Printf("nats reconnected: %s", nc.ConnectedUrl())
		}),
		nats.ClosedHandler(func(_ *nats.Conn) {
			log.Printf("nats connection closed")
		}),
	)
	if err != nil {
		log.Fatalf("nats connect error: %v", err)
	}
	defer nc.Close()

	js, err := nc.JetStream(nats.Domain(jsDomain))
	if err != nil {
		log.Fatalf("jetstream error: %v", err)
	}

	{
		pctx, c := context.WithTimeout(ctx, maxWait)
		defer c()
		if _, err := js.StreamInfo("uploads", nats.Context(pctx)); err != nil {
			if _, err = js.AddStream(&nats.StreamConfig{
				Name:       "uploads",
				Subjects:   []string{"b_log.uploaded"},
				Storage:    nats.FileStorage,
				Retention:  nats.LimitsPolicy,
				Duplicates: 2 * time.Minute,
			}, nats.Context(pctx)); err != nil {
				log.Fatalf("add stream error: %v", err)
			}
		}
	}

	sub, err := infra.SubscribeUploads(js, "b_log.uploaded", "file_resolver_worker",
		func(msgID string, ev infra.UploadEvent) error {
			log.Printf(
				"upload msg=%q stored=%q orig=%q size=%d path=%q at=%s",
				msgID, ev.StoredName, ev.OriginalName, ev.Size, ev.StoredPath, ev.UploadedAt.UTC().Format(time.RFC3339),
			)
			return proc.Handle(msgID, ev)
		})
	if err != nil {
		log.Fatalf("subscribe error: %v", err)
	}
	defer func() {
		dctx, c := context.WithTimeout(context.Background(), maxWait)
		defer c()
		done := make(chan struct{})
		go func() {
			_ = sub.Drain()
			close(done)
		}()
		select {
		case <-done:
		case <-dctx.Done():
			log.Printf("WARN: subscription drain timed out")
		}
	}()

	log.Printf("md-worker ready | nats=%s js_domain=%s shared_dir=%s", natsURL, jsDomain, sharedDir)

	<-ctx.Done()

	{
		dctx, c := context.WithTimeout(context.Background(), maxWait)
		defer c()
		done := make(chan struct{})
		go func() {
			_ = nc.Drain()
			close(done)
		}()
		select {
		case <-done:
		case <-dctx.Done():
			log.Printf("WARN: nats drain timed out")
		}
	}
}
