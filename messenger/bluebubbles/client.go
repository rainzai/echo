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
		Method:   "private-api",
		TempGUID: uuid.New().String(),
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/message/text", req, nil); err != nil {
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
		With:  []string{"chat", "handle", "attachment"},
	}
	var res struct {
		Data []Message `json:"data"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v1/message/query", req, &res); err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	return res.Data, nil
}

// Attachment downloads a file, BlueBubbles converts HEIC photos to JPEG
func (c *Client) Attachment(ctx context.Context, guid string) ([]byte, error) {
	var b []byte
	if err := c.do(ctx, http.MethodGet, "/api/v1/attachment/"+url.PathEscape(guid)+"/download", nil, &b); err != nil {
		return nil, fmt.Errorf("download attachment: %w", err)
	}
	return b, nil
}

type reactRequest struct {
	ChatGUID            string `json:"chatGuid"`
	SelectedMessageGUID string `json:"selectedMessageGuid"`
	Reaction            string `json:"reaction"`
}

// React sends a tapback: love, like, dislike, laugh, emphasize or question.
func (c *Client) React(ctx context.Context, chatGUID, messageGUID, reaction string) error {
	req := reactRequest{ChatGUID: chatGUID, SelectedMessageGUID: messageGUID, Reaction: reaction}
	if err := c.do(ctx, http.MethodPost, "/api/v1/message/react", req, nil); err != nil {
		return fmt.Errorf("react: %w", err)
	}
	return nil
}

func (c *Client) Typing(ctx context.Context, chatGUID string, on bool) error {
	method := http.MethodPost
	if !on {
		method = http.MethodDelete
	}
	if err := c.do(ctx, method, "/api/v1/chat/"+url.PathEscape(chatGUID)+"/typing", nil, nil); err != nil {
		return fmt.Errorf("typing: %w", err)
	}
	return nil
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}

	u := c.baseURL + path + "?password=" + url.QueryEscape(c.password)
	req, err := http.NewRequestWithContext(ctx, method, u, r)
	if err != nil {
		return errors.New("invalid request")
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

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

	switch out := out.(type) {
	case nil:
		return nil
	case *[]byte:
		*out, err = io.ReadAll(io.LimitReader(res.Body, 20<<20))
		return err
	default:
		return json.NewDecoder(res.Body).Decode(out)
	}
}
