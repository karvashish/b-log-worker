package worker

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"b-log.com/b-log-worker/internal/infra"
)

type Processor struct {
	DB        *sql.DB
	SharedDir string
}

const (
	opTimeout = 5 * time.Second
)

func New(db *sql.DB, sharedDir string) (*Processor, error) {
	if db == nil {
		return nil, errors.New("nil db")
	}
	if sharedDir == "" {
		return nil, errors.New("empty sharedDir")
	}

	abs, err := filepath.Abs(sharedDir)
	if err != nil {
		return nil, fmt.Errorf("abs sharedDir: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat sharedDir: %w", err)
	}
	if !st.IsDir() {
		return nil, &os.PathError{Op: "stat", Path: abs, Err: os.ErrInvalid}
	}
	return &Processor{DB: db, SharedDir: abs}, nil
}

func (p *Processor) Handle(msgID string, ev infra.UploadEvent) error {

	if ev.StoredPath != "" {
		if st, err := os.Stat(ev.StoredPath); err == nil && st.Mode().IsRegular() {
			log.Printf("stored_path ok: %s size=%d", ev.StoredPath, st.Size())
		} else {
			log.Printf("WARN stored_path not foun: %s", ev.StoredPath)
		}
	}

	storedName := filepath.Base(ev.StoredName)
	local := filepath.Join(p.SharedDir, storedName)

	if st, err := os.Stat(local); err == nil && st.Mode().IsRegular() {
		log.Printf(
			"resolved file: %s | msg=%q orig=%q size=%d at=%s",
			local, msgID, ev.OriginalName, ev.Size, ev.UploadedAt.UTC().Format(time.RFC3339),
		)

		b, err := os.ReadFile(local)
		if err != nil {
			log.Printf("ERROR reading file %s: %v", local, err)
			return err
		}

		title := strings.TrimSpace(ev.OriginalName)
		content := string(b)

		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()

		_, err = p.DB.ExecContext(ctx,
			"INSERT INTO posts (title, content) VALUES ($1, $2)",
			title, content,
		)
		if err != nil {
			log.Printf("DB insert error (posts): %v", err)
			return err
		}
		log.Printf("DB insert ok: posts(title=%q, bytes=%d)", title, len(content))
		return nil
	}

	log.Printf("ERROR local file missing: %s (shared=%s stored_name=%q)", local, p.SharedDir, ev.StoredName)
	return os.ErrNotExist
}
