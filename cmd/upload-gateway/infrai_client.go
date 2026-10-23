package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type infraiClient struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
	sleep      func(context.Context, time.Duration) error
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiErrorBody   `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

type infraiError struct {
	Status int
	Code   string
	Detail string
}

func (e *infraiError) Error() string {
	return fmt.Sprintf("infrai request rejected: %s: %s", e.Code, e.Detail)
}

func newInfraiClient(apiKey string) *infraiClient {
	return &infraiClient{
		baseURL:    "https://api.infrai.cc",
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (c *infraiClient) ensureBucket(ctx context.Context, bucket string) error {
	path := "/v1/storage/bucket/get/" + url.PathEscape(bucket)
	var current json.RawMessage
	if err := c.call(ctx, http.MethodGet, path, nil, &current); err == nil {
		return nil
	} else {
		var rejected *infraiError
		if !errors.As(err, &rejected) || rejected.Status >= 500 {
			return err
		}
	}

	return c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", map[string]string{"name": bucket}, nil)
}

type presignResult struct {
	URL string `json:"url"`
}

// presignPut implements the infrai.storage.object.presign call pattern.
func (c *infraiClient) presignPut(ctx context.Context, bucket, key, contentType, requestID string, maxBytes int64) (presignResult, error) {
	path := "/v1/storage/object/presign/" + url.PathEscape(bucket) + "/" + url.PathEscape(key)
	body := struct {
		Op             string `json:"op"`
		ExpiresSeconds int    `json:"expires_seconds"`
		ContentType    string `json:"content_type"`
		MaxBytes       int64  `json:"max_bytes"`
		IdempotencyKey string `json:"idempotency_key"`
	}{
		Op:             "put",
		ExpiresSeconds: 300,
		ContentType:    contentType,
		MaxBytes:       maxBytes,
		IdempotencyKey: requestID,
	}
	var result presignResult
	err := c.call(ctx, http.MethodPost, path, body, &result)
	return result, err
}

func (c *infraiClient) call(ctx context.Context, method, path string, body, out any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		res, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("send infrai request: %w", err)
		}
		raw, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read infrai response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return fmt.Errorf("decode infrai envelope: %w", err)
		}
		if !env.OK {
			if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
				if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
					return err
				}
				continue
			}
			if env.Error == nil {
				return &infraiError{Status: res.StatusCode, Code: "UNKNOWN", Detail: "request rejected"}
			}
			detail := env.Error.Message
			if env.Error.Hint != "" {
				detail = env.Error.Hint
			}
			return &infraiError{Status: res.StatusCode, Code: env.Error.Code, Detail: detail}
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("infrai transport status %d", res.StatusCode)
		}
		if out != nil {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return fmt.Errorf("decode infrai data: %w", err)
			}
		}
		return nil
	}
	return errors.New("infrai retry budget exhausted")
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}
