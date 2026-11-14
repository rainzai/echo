package check

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

var ErrNoPrice = errors.New("no price found")

type Request struct {
	Query     string `json:"query"`
	Country   string `json:"country,omitempty"`
	URL       string `json:"url,omitempty"`
	Shop      string `json:"shop,omitempty"`
	Condition string `json:"condition,omitempty"`
}

type Result struct {
	Query           string       `json:"query"`
	Country         string       `json:"country"`
	URL             string       `json:"url"`
	Name            string       `json:"name"`
	Currency        string       `json:"currency"`
	Price           float64      `json:"price"`
	Low30d          *float64     `json:"low_30d"`
	PriceDoubt      string       `json:"price_doubt"`
	Condition       string       `json:"condition"`
	Bundle          bool         `json:"bundle"`
	ShopFound       bool         `json:"shop_found"`
	ClaimedPrice    *float64     `json:"claimed_price"`
	ClaimedDiscount *float64     `json:"claimed_discount"`
	FakeSale        bool         `json:"fake_sale"`
	History         []PricePoint `json:"history"`
	Elsewhere       []Offer      `json:"elsewhere"`
	Sources         []Source     `json:"sources"`
}

type PricePoint struct {
	Date  string  `json:"date"`
	Price float64 `json:"price"`
	URL   string  `json:"url"`
}

type Offer struct {
	URL   string  `json:"url"`
	Shop  string  `json:"shop"`
	Price float64 `json:"price"`
	Date  string  `json:"date"`
	Doubt string  `json:"doubt"`
}

type Source struct {
	N     int    `json:"n"`
	URL   string `json:"url"`
	Date  string `json:"date"`
	Label string `json:"label"`
}

type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{}}
}

func (c *Client) Check(ctx context.Context, req Request) (Result, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return Result{}, err
	}

	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/check", bytes.NewReader(b))
	if err != nil {
		return Result{}, err
	}
	r.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(r)
	if err != nil {
		return Result{}, fmt.Errorf("check: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusUnprocessableEntity {
		return Result{}, ErrNoPrice
	}
	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return Result{}, fmt.Errorf("check: %s: %s", res.Status, msg)
	}

	var out Result
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("check: %w", err)
	}
	return out, nil
}
