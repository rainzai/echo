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

	return Result{
		Verdict:  FakeSale,
		Summary:  "This '-40%' price was the normal price 30 days ago.",
		PriceNow: 79.99,
		Currency: "EUR",
		PriceHistory: []PricePoint{
			{Date: "2026-08-16", Price: 79.99, Source: "https://shop.example/product/123"},
			{Date: "2026-09-15", Price: 89.99, Source: "https://shop.example/product/123"},
			{Date: "2026-10-15", Price: 79.99, Source: "https://shop.example/product/123"},
		},
		CheaperElsewhere: []Offer{
			{Shop: "other.example", Price: 69.99, URL: "https://other.example/item/456"},
		},
	}, nil
}
