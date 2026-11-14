package check

import (
	"context"
	"time"
)

type Fake struct{}

func (Fake) Check(ctx context.Context, req Request) (Result, error) {
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-time.After(3 * time.Second):
	}

	low := 79.99
	return Result{
		Query:    req.Query,
		Country:  "NL",
		URL:      "https://shop.example/product/123",
		Name:     "Sony WH-1000XM5",
		Currency: "EUR",
		Price:    79.99,
		Low30d:   &low,
		FakeSale: true,
		History: []PricePoint{
			{Date: "2026-09-15", Price: 89.99, URL: "https://shop.example/product/123"},
			{Date: "2026-10-15", Price: 79.99, URL: "https://shop.example/product/123"},
			{Date: "2026-11-14", Price: 79.99, URL: "https://shop.example/product/123"},
		},
		Elsewhere: []Offer{
			{URL: "https://other.example/item/456", Shop: "other.example", Price: 69.99, Date: "2026-11-14"},
		},
		Sources: []Source{
			{N: 1, URL: "https://shop.example/product/123", Date: "2026-10-15", Label: "shop.example on 2026-10-15"},
			{N: 2, URL: "https://other.example/item/456", Date: "2026-11-14", Label: "other.example"},
		},
	}, nil
}
