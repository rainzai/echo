package agent

import (
	"fmt"
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
			"url": map[string]any{
				"type":        "string",
				"description": "The product link the user sent, exactly as sent.",
			},
			"shop": map[string]any{
				"type":        "string",
				"description": "The website of the shop the user named or the photo shows, e.g. coolblue.nl or bol.com.",
			},
			"buy_word": map[string]any{
				"type":        "string",
				"description": "The word for buy in the country's own main language, never English unless the country speaks only English, even when the user writes English. E.g. kopen, kaufen, kupić, खरीदें, خریدیں, কিনুন.",
			},
			"condition": map[string]any{
				"type":        "string",
				"enum":        []string{"new", "refurbished", "used"},
				"description": "Only when the user or the photo says it's refurbished or used.",
			},
		},
		"required": []string{"query", "buy_word"},
	}},
}

// facts does the math so the model never has to
func facts(req check.Request, r check.Result) map[string]any {
	f := map[string]any{
		"product":  r.Name,
		"shop":     shop(r.URL),
		"page":     r.URL,
		"currency": r.Currency,
		"price":    r.Price,
	}
	if r.Condition != "" && r.Condition != "new" {
		f["condition"] = r.Condition
	}
	if r.Bundle {
		f["bundle"] = true
	}
	// shop_found compares a domain, gemini often passes a name like "Coolblue"
	named := strings.ToLower(strings.ReplaceAll(req.Shop, " ", ""))
	if req.Shop != "" && req.URL == "" && !r.ShopFound && !strings.Contains(shop(r.URL), named) {
		f["shop_note"] = "couldn't find it at " + req.Shop + ", this price is from " + shop(r.URL)
	}
	if doubt := doubts[r.PriceDoubt]; doubt != "" {
		f["price_doubt"] = doubt
	}

	low := r.Low30d
	if low != nil && *low > 0 {
		pct := (r.Price - *low) / *low * 100
		f["lowest_30_days"] = *low
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

	// 30 days is what the eu discount rule uses, the history goes back 90 days for the bigger picture
	if len(r.History) > 1 {
		var points []string
		for _, p := range r.History {
			points = append(points, fmt.Sprintf("%s: %.2f", p.Date, p.Price))
		}
		f["price_history"] = points
		low, when := lowest(r.History, r.Price)
		f["lowest_in_history"] = low
		if when == "" {
			f["history_check"] = "today is the lowest price in the whole history"
		} else {
			f["history_check"] = "it was cheaper on " + when
		}
	}

	if r.ClaimedPrice != nil || r.ClaimedDiscount != nil {
		if r.ClaimedPrice != nil {
			f["claimed_old_price"] = *r.ClaimedPrice
		}
		if r.ClaimedDiscount != nil {
			f["claimed_discount_percent"] = *r.ClaimedDiscount
		}
		switch {
		case low == nil:
			f["claim_check"] = "can't verify the discount, no older prices found"
		case *low <= r.Price*1.03:
			f["claim_check"] = "the discount isn't real: it was this price or cheaper in the last 30 days"
		default:
			f["claim_check"] = "the discount is real: it's below the lowest price of the last 30 days"
		}
	}

	var cheaper []map[string]any
	for _, o := range r.Elsewhere {
		c := map[string]any{"shop": o.Shop, "price": o.Price, "page": o.URL}
		if doubt := doubts[o.Doubt]; doubt != "" {
			c["price_doubt"] = doubt
		}
		cheaper = append(cheaper, c)
	}
	if len(cheaper) > 0 {
		f["cheaper_elsewhere"] = cheaper
	}
	return f
}

// lowest finds the cheapest earlier point in the history, empty when today is the cheapest
func lowest(history []check.PricePoint, now float64) (float64, string) {
	low, when := now, ""
	for _, p := range history {
		if p.Price > 0 && p.Price < low {
			low, when = p.Price, p.Date
		}
	}
	return low, when
}

var doubts = map[string]string{
	"unverified":       "only from the shop's own product data, not confirmed in the page text",
	"disagree":         "the page and the shop's data show different prices",
	"far_below_others": "much cheaper than other shops, maybe without VAT, another variant or a misread",
}

func shop(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}
