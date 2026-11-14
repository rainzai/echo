package agent

import (
	"math"
	"net/url"
	"strings"

	"messenger/check"
)

var tools = []Tool{{
	Name:        "check_deal",
	Description: "Look up the current price, the lowest price in the last 30 days and cheaper shops for a product.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The product name with brand and model number, only words from the message or link, nothing you add yourself.",
			},
			"country": map[string]any{
				"type":        "string",
				"description": "Two letter country code, only if the user said where they shop.",
			},
		},
		"required": []string{"query"},
	}},
}

// facts gives the model the numbers it needs, worked out here so it never
// has to calculate anything itself
func facts(r check.Result) map[string]any {
	f := map[string]any{
		"product":  r.Name,
		"shop":     shop(r.URL),
		"currency": r.Currency,
		"price":    r.Price,
	}

	if r.Low30d != nil && *r.Low30d > 0 {
		low := *r.Low30d
		pct := (r.Price - low) / low * 100
		f["lowest_30_days"] = low
		f["percent_above_lowest"] = math.Round(pct*10) / 10
		switch {
		case pct <= -3:
			f["price_check"] = "real drop: clearly below the lowest price of the last 30 days"
		case pct < 3:
			f["price_check"] = "normal price: about the same as the lowest price of the last 30 days"
		default:
			f["price_check"] = "not a deal: it was cheaper in the last 30 days"
		}
	} else {
		f["price_check"] = "no older prices found to compare"
	}

	var cheaper []map[string]any
	for _, o := range r.Elsewhere {
		cheaper = append(cheaper, map[string]any{"shop": o.Shop, "price": o.Price})
	}
	if len(cheaper) > 0 {
		f["cheaper_elsewhere"] = cheaper
	}
	return f
}

func shop(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
