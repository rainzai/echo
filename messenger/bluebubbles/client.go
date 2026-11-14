package bluebubbles

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
	"uuid"
)

type Client struct {
	baseURL  string
	password string
	http     *http.Client
}

func NewClient(baseURL, password string) *Client {
	return &Client{baseURL: baseURL, password: password, http: &http.Client{}}
}

type sendTextRequest struct {
	ChatGUID string `json:"chatGuid"`
	Message  string `json:"message"`
	Method   string `json:"method"`
	TempGUID string `json:"tempGuid"`
}

func (c *Client) SendText(ctx context.Context, chatGUID, text string) error {
	req := sendTextRequest{
		ChatGUID: chatGUID,
		Message:  text,
		Method:   "apple-script",
		TempGUID: uuid.New().String(),
	}
	if err := c.post(ctx, "/api/v1/message/text", req, nil); err != nil {
		return fmt.Errorf("send text: %w", err)
	}
	return nil
}

type queryRequest struct {
	After int64    `json:"after"`
	Limit int      `json:"limit"`
	Sort  string   `json:"sort"`
	With  []string `json:"with"`
}

func (c *Client) Messages(ctx context.Context, after time.Time) ([]Message, error) {
	req := queryRequest{
		After: after.UnixMilli(),
		Limit: 100,
		Sort:  "ASC",
		With:  []string{"chat", "handle"},
	}
	var res struct {
		Data []Message `json:"data"`
	}
	if err := c.post(ctx, "/api/v1/message/query", req, &res); err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	return res.Data, nil
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	u := c.baseURL + path + "?password=" + url.QueryEscape(c.password)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return errors.New("invalid request")
	}
	req.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(req)
	if err != nil {
		// url.Error includes the full URL, which has the password in it
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("%s: %s", res.Status, msg)
	}

	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}
