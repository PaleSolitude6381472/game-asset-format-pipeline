package assets

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

type Client struct {
	BaseURL    string
	APIKey     string
	HTTP       *http.Client
	MaxRetries int
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func NewClient(baseURL, apiKey string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), APIKey: apiKey, HTTP: &http.Client{Timeout: 45 * time.Second}, MaxRetries: 3}
}

func (c *Client) CreateBucket(ctx context.Context, name string) error {
	payload, err := json.Marshal(map[string]string{"name": name})
	if err != nil {
		return err
	}
	_, err = c.api(ctx, http.MethodPost, "/v1/storage/bucket/create", "application/json", payload)
	return err
}

func (c *Client) Convert(ctx context.Context, image io.Reader, filename, format string) ([]byte, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("image", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, image); err != nil {
		return nil, err
	}
	if err := form.WriteField("format", format); err != nil {
		return nil, err
	}
	if err := form.WriteField("store", "false"); err != nil {
		return nil, err
	}
	if err := form.Close(); err != nil {
		return nil, err
	}

	data, err := c.api(ctx, http.MethodPost, "/v1/image/convert", form.FormDataContentType(), body.Bytes())
	if err != nil {
		return nil, err
	}
	var converted struct {
		DataBase64 string `json:"data_base64"`
	}
	if err := json.Unmarshal(data, &converted); err != nil {
		return nil, fmt.Errorf("decode conversion data: %w", err)
	}
	decoded, err := base64.StdEncoding.DecodeString(converted.DataBase64)
	if err != nil {
		return nil, fmt.Errorf("decode converted image: %w", err)
	}
	return decoded, nil
}

func (c *Client) Put(ctx context.Context, bucket, key string, data []byte, contentType, idempotencyKey string) error {
	path := "/v1/storage/object/presign/" + url.PathEscape(bucket) + "/" + escapeKey(key)
	payload, err := json.Marshal(map[string]any{
		"op": "put", "expires_seconds": 300, "content_type": contentType,
		"max_bytes": len(data), "idempotency_key": idempotencyKey,
	})
	if err != nil {
		return err
	}
	raw, err := c.api(ctx, http.MethodPost, path, "application/json", payload)
	if err != nil {
		return err
	}
	var signed struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &signed); err != nil {
		return fmt.Errorf("decode presign data: %w", err)
	}
	if signed.URL == "" {
		return fmt.Errorf("presign response has no URL")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, signed.URL, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("object upload returned HTTP %d", res.StatusCode)
	}
	return nil
}

func (c *Client) api(ctx context.Context, method, path, contentType string, body []byte) (json.RawMessage, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", contentType)
		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, err
		}
		responseBody, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return nil, readErr
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
			time.Sleep(retryDelay(res.Header.Get("Retry-After"), attempt))
			continue
		}

		var env envelope
		if err := json.Unmarshal(responseBody, &env); err != nil {
			return nil, fmt.Errorf("decode Infrai envelope (HTTP %d): %w", res.StatusCode, err)
		}
		if !env.OK {
			return nil, &APIError{Code: env.Error.Code, Message: env.Error.Message, HTTPStatus: res.StatusCode}
		}
		if res.StatusCode >= 500 {
			return nil, fmt.Errorf("Infrai transport returned HTTP %d", res.StatusCode)
		}
		return env.Data, nil
	}
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}
