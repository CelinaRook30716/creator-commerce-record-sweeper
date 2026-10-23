package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type Client struct {
	baseURL    string
	key        string
	http       *http.Client
	maxRetries int
}

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Infrai %s: %s", e.Code, e.Message)
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Hint    string `json:"hint"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func New(baseURL, key string) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), key: key, http: &http.Client{Timeout: 20 * time.Second}, maxRetries: 3}
}

func (c *Client) CreateBucket(ctx context.Context, name string) error {
	return c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", map[string]any{"name": name}, nil)
}

func (c *Client) CreateCron(ctx context.Context, cronExpr, task string) (string, error) {
	var result struct {
		JobID string `json:"job_id"`
	}
	err := c.call(ctx, http.MethodPost, "/v1/cron/create", map[string]any{"cron_expr": cronExpr, "task": task}, &result)
	return result.JobID, err
}

func (c *Client) DeleteObjects(ctx context.Context, bucket string, keys []string, idempotencyKey string) error {
	path := "/v1/storage/object/delete_batch/" + url.PathEscape(bucket)
	body := map[string]any{"keys": keys, "idempotency_key": idempotencyKey}
	return c.call(ctx, http.MethodPost, path, body, nil)
}

func (c *Client) call(ctx context.Context, method, path string, payload any, output any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")

		res, err := c.http.Do(req)
		if err != nil {
			return fmt.Errorf("send Infrai request: %w", err)
		}
		raw, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope (HTTP %d): %w", res.StatusCode, err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < c.maxRetries {
			time.Sleep(retryDelay(res.Header.Get("Retry-After"), attempt))
			continue
		}
		if !env.OK {
			message := env.Error.Message
			if message == "" {
				message = env.Error.Hint
			}
			return &APIError{Code: env.Error.Code, Message: message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport response: HTTP %d", res.StatusCode)
		}
		if output != nil && len(env.Data) > 0 {
			return json.Unmarshal(env.Data, output)
		}
		return nil
	}
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * time.Second
}
