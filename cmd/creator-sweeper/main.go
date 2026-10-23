package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/example/creator-commerce-sweeper/internal/infrai"
	"github.com/example/creator-commerce-sweeper/internal/sweep"
)

type service struct {
	mu         sync.Mutex
	client     *infrai.Client
	bucket     string
	ledgerPath string
	now        func() time.Time
}

type sweepResult struct {
	Scanned int      `json:"scanned"`
	Deleted int      `json:"deleted"`
	IDs     []string `json:"ids"`
}

func main() {
	key := mustEnv("INFRAI_API_KEY")
	bucket := envOr("CREATOR_ASSET_BUCKET", "creator-sweep-assets")
	client := infrai.New(envOr("INFRAI_BASE_URL", infrai.DefaultBaseURL), key)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := client.CreateBucket(ctx, bucket); err != nil {
		log.Fatal(err)
	}

	svc := &service{client: client, bucket: bucket, ledgerPath: envOr("CREATOR_LEDGER", "records.json"), now: time.Now}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /sweep", svc.handleSweep)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	if taskURL := os.Getenv("SWEEP_TASK_URL"); taskURL != "" {
		jobID, err := client.CreateCron(context.Background(), envOr("SWEEP_CRON", "17 * * * *"), strings.TrimRight(taskURL, "/")+"/sweep")
		if err != nil {
			log.Fatal(err)
		}
		log.Printf("scheduled cleanup job_id=%s", jobID)
	}

	addr := envOr("ADDR", ":8080")
	log.Printf("creator cleanup listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func (s *service) handleSweep(w http.ResponseWriter, r *http.Request) {
	result, err := s.runSweep(r.Context())
	if err != nil {
		status := http.StatusInternalServerError
		var apiErr *infrai.APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus >= 400 && apiErr.HTTPStatus < 500 {
			status = apiErr.HTTPStatus
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *service) runSweep(ctx context.Context) (sweepResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	records, err := readLedger(s.ledgerPath)
	if err != nil {
		return sweepResult{}, err
	}
	stale := sweep.SelectStale(records, s.now().UTC())
	result := sweepResult{Scanned: len(records), Deleted: len(stale), IDs: make([]string, 0, len(stale))}
	if len(stale) == 0 {
		return result, nil
	}

	keys := make([]string, 0, len(stale))
	removed := make(map[string]bool, len(stale))
	for _, record := range stale {
		keys = append(keys, record.ObjectKey)
		result.IDs = append(result.IDs, record.ID)
		removed[record.ID] = true
	}
	sum := sha256.Sum256([]byte(strings.Join(result.IDs, "\n")))
	if err := s.client.DeleteObjects(ctx, s.bucket, keys, "creator-sweep-"+hex.EncodeToString(sum[:12])); err != nil {
		return sweepResult{}, err
	}

	kept := records[:0]
	for _, record := range records {
		if !removed[record.ID] {
			kept = append(kept, record)
		}
	}
	if err := writeLedger(s.ledgerPath, kept); err != nil {
		return sweepResult{}, err
	}
	return result, nil
}

func readLedger(path string) ([]sweep.Record, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ledger: %w", err)
	}
	var records []sweep.Record
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("decode ledger: %w", err)
	}
	return records, nil
}

func writeLedger(path string, records []sweep.Record) error {
	raw, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	temporary := path + ".next"
	if err := os.WriteFile(temporary, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, filepath.Clean(path))
}

func mustEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
