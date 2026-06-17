package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type MaxClient struct {
	apiURL             string
	token              string
	chatID             int64
	dryRun             bool
	messageFormat      string
	disableLinkPreview bool
	uploadRetryCount   int
	uploadRetryDelay   time.Duration
	httpClient         *http.Client
}

type MaxAttachment struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

type MaxSendMessageRequest struct {
	Text        string          `json:"text,omitempty"`
	Attachments []MaxAttachment `json:"attachments,omitempty"`
	Notify      bool            `json:"notify"`
	Format      string          `json:"format,omitempty"`
}

func NewMaxClient(cfg Config) *MaxClient {
	return &MaxClient{
		apiURL:             cfg.MaxAPIURL,
		token:              cfg.MaxToken,
		chatID:             cfg.MaxChatID,
		dryRun:             cfg.DryRun,
		messageFormat:      cfg.MaxMessageFormat,
		disableLinkPreview: cfg.MaxDisableLinkPreview,
		uploadRetryCount:   cfg.MaxUploadRetryCount,
		uploadRetryDelay:   cfg.MaxUploadRetryDelay,
		httpClient:         &http.Client{Timeout: 90 * time.Second},
	}
}

func (c *MaxClient) SendMessage(ctx context.Context, text string, attachments []MaxAttachment) error {
	if c.dryRun {
		fmt.Println("\n--- DRY_RUN: MAX message ---")
		fmt.Println(text)
		if len(attachments) > 0 {
			fmt.Printf("attachments: %+v\n", attachments)
		}
		fmt.Println("--- end ---")
		return nil
	}

	q := url.Values{}
	q.Set("chat_id", strconv.FormatInt(c.chatID, 10))
	q.Set("disable_link_preview", strconv.FormatBool(c.disableLinkPreview))
	endpoint := c.apiURL + "/messages?" + q.Encode()
	body := MaxSendMessageRequest{Text: text, Attachments: attachments, Notify: true, Format: c.messageFormat}
	return c.postJSONWithRetry(ctx, endpoint, body, nil)
}

func (c *MaxClient) Test(ctx context.Context) error {
	return c.SendMessage(ctx, "✅ Тестовое сообщение от Cropwise → MAX bot", nil)
}

func (c *MaxClient) UploadImageFromURL(ctx context.Context, imageURL string) (MaxAttachment, error) {
	return c.UploadImageFromURLWithHeaders(ctx, imageURL, nil)
}

func (c *MaxClient) UploadImageFromURLWithHeaders(ctx context.Context, imageURL string, headers map[string]string) (MaxAttachment, error) {
	if c.dryRun {
		return MaxAttachment{Type: "image", Payload: map[string]any{"dry_run_url": imageURL}}, nil
	}

	imgReq, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return MaxAttachment{}, err
	}
	for k, v := range headers {
		if strings.TrimSpace(k) != "" && strings.TrimSpace(v) != "" {
			imgReq.Header.Set(k, v)
		}
	}
	imgResp, err := c.httpClient.Do(imgReq)
	if err != nil {
		return MaxAttachment{}, err
	}
	defer imgResp.Body.Close()
	if imgResp.StatusCode < 200 || imgResp.StatusCode >= 300 {
		return MaxAttachment{}, fmt.Errorf("download image %s: HTTP %d", imageURL, imgResp.StatusCode)
	}
	imgBytes, err := io.ReadAll(imgResp.Body)
	if err != nil {
		return MaxAttachment{}, err
	}

	uploadInfo, err := c.createUpload(ctx, "image")
	if err != nil {
		return MaxAttachment{}, err
	}
	uploadURL, _ := uploadInfo["url"].(string)
	if uploadURL == "" {
		return MaxAttachment{}, fmt.Errorf("MAX upload response has empty url: %+v", uploadInfo)
	}

	payload, err := c.multipartUpload(ctx, uploadURL, filenameFromURL(imageURL), imgBytes)
	if err != nil {
		return MaxAttachment{}, err
	}

	imagePayload, err := normalizeMaxImageUploadPayload(payload, uploadInfo, uploadURL)
	if err != nil {
		return MaxAttachment{}, err
	}

	return MaxAttachment{Type: "image", Payload: imagePayload}, nil
}

func (c *MaxClient) UploadImageBytes(ctx context.Context, filename string, imgBytes []byte) (MaxAttachment, error) {
	if c.dryRun {
		return MaxAttachment{Type: "image", Payload: map[string]any{"dry_run_filename": filename}}, nil
	}

	uploadInfo, err := c.createUpload(ctx, "image")
	if err != nil {
		return MaxAttachment{}, err
	}
	uploadURL, _ := uploadInfo["url"].(string)
	if uploadURL == "" {
		return MaxAttachment{}, fmt.Errorf("MAX upload response has empty url: %+v", uploadInfo)
	}

	payload, err := c.multipartUpload(ctx, uploadURL, filename, imgBytes)
	if err != nil {
		return MaxAttachment{}, err
	}
	imagePayload, err := normalizeMaxImageUploadPayload(payload, uploadInfo, uploadURL)
	if err != nil {
		return MaxAttachment{}, err
	}
	return MaxAttachment{Type: "image", Payload: imagePayload}, nil
}

func normalizeMaxImageUploadPayload(uploadResp map[string]any, uploadInfo map[string]any, uploadURL string) (map[string]any, error) {
	// По документации MAX для image payload может быть одним из вариантов:
	// {"token":"..."} или {"photos":{...}}. На практике upload-сервер может вернуть
	// token в ответе загрузки, в первом ответе /uploads или внутри URL загрузки.
	if payload, ok := extractImagePayload(uploadResp); ok {
		return payload, nil
	}
	if payload, ok := extractImagePayload(uploadInfo); ok {
		return payload, nil
	}
	if token := tokenFromUploadURL(uploadURL); token != "" {
		return map[string]any{"token": token}, nil
	}
	return nil, fmt.Errorf("MAX image upload payload has no token/photos: upload_response=%+v upload_info=%+v", uploadResp, uploadInfo)
}

func extractImagePayload(m map[string]any) (map[string]any, bool) {
	if m == nil {
		return nil, false
	}
	if token := strings.TrimSpace(anyToString(m["token"])); token != "" {
		return map[string]any{"token": token}, true
	}
	if photos, ok := m["photos"]; ok && photos != nil {
		return map[string]any{"photos": photos}, true
	}
	for _, key := range []string{"photo", "image", "payload", "retval", "result", "data"} {
		if child, ok := m[key].(map[string]any); ok {
			if payload, ok := extractImagePayload(child); ok {
				return payload, true
			}
		}
	}
	return nil, false
}

func tokenFromUploadURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	for _, key := range []string{"token", "photo_token", "attachment_token", "upload_token"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			return v
		}
	}
	return ""
}

func (c *MaxClient) createUpload(ctx context.Context, typ string) (map[string]any, error) {
	q := url.Values{}
	q.Set("type", typ)
	endpoint := c.apiURL + "/uploads?" + q.Encode()
	var out map[string]any
	if err := c.postJSON(ctx, endpoint, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *MaxClient) multipartUpload(ctx context.Context, uploadURL, filename string, data []byte) (map[string]any, error) {
	var b bytes.Buffer
	w := multipart.NewWriter(&b)
	fw, err := w.CreateFormFile("data", filename)
	if err != nil {
		return nil, err
	}
	if _, err := fw.Write(data); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, &b)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MAX multipart upload: HTTP %d: %s", resp.StatusCode, trimForLog(string(respBytes), 1000))
	}
	var out map[string]any
	if err := json.Unmarshal(respBytes, &out); err != nil {
		return nil, fmt.Errorf("MAX upload decode: %w; body=%s", err, trimForLog(string(respBytes), 1000))
	}
	return out, nil
}

func (c *MaxClient) postJSONWithRetry(ctx context.Context, endpoint string, body any, out any) error {
	attempts := c.uploadRetryCount + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			delay := c.uploadRetryDelay
			if delay <= 0 {
				delay = 2 * time.Second
			}
			timer := time.NewTimer(delay * time.Duration(i))
			select {
			case <-ctx.Done():
				if !timer.Stop() {
					<-timer.C
				}
				return ctx.Err()
			case <-timer.C:
			}
		}
		err := c.postJSON(ctx, endpoint, body, out)
		if err == nil {
			return nil
		}
		lastErr = err
		// MAX иногда возвращает attachment.not.ready сразу после загрузки файла.
		if !strings.Contains(strings.ToLower(err.Error()), "attachment.not.ready") && !strings.Contains(strings.ToLower(err.Error()), "not.processed") {
			break
		}
	}
	return lastErr
}

func (c *MaxClient) postJSON(ctx context.Context, endpoint string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("MAX POST %s: HTTP %d: %s", endpoint, resp.StatusCode, trimForLog(string(b), 1000))
	}
	if out != nil && len(b) > 0 {
		if err := json.Unmarshal(b, out); err != nil {
			return fmt.Errorf("MAX decode response: %w; body=%s", err, trimForLog(string(b), 1000))
		}
	}
	return nil
}

func filenameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err == nil {
		base := filepath.Base(u.Path)
		if base != "." && base != "/" && strings.Contains(base, ".") {
			return base
		}
	}
	return fmt.Sprintf("cropwise_%d.jpg", time.Now().UnixNano())
}
