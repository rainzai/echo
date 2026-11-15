package reply

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"messenger/check"
)

func Verdict(r check.Result) string {
	var b strings.Builder

	switch {
	case r.FakeSale:
		fmt.Fprintf(&b, "Fake sale. %s is %s, but it was %s or less in the last 30 days.\n",
			r.Name, price(r.Price, r.Currency), price(*r.Low30d, r.Currency))
	case r.Low30d != nil:
		fmt.Fprintf(&b, "Real price drop. %s is %s, lowest in the last 30 days was %s.\n",
			r.Name, price(r.Price, r.Currency), price(*r.Low30d, r.Currency))
	default:
		fmt.Fprintf(&b, "%s is %s. I couldn't find older prices to compare.\n",
			r.Name, price(r.Price, r.Currency))
	}

	for _, o := range r.Elsewhere {
		fmt.Fprintf(&b, "Cheaper at %s: %s\n", o.Shop, price(o.Price, r.Currency))
	}

	return strings.TrimSpace(b.String())
}

// Sources shows the prices behind the last check, with the page each one came from
func Sources(r check.Result) string {
	if r.URL == "" {
		return "I don't have any links yet. Send me a product first!"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s at %s\n", trimShop(r.Name, shop(r.URL)), shop(r.URL))
	for i, p := range r.History {
		now := ""
		if i == len(r.History)-1 {
			now = " (now)"
		}
		fmt.Fprintf(&b, "%s: %s%s\n", day(p.Date), price(p.Price, r.Currency), now)
	}
	if len(r.History) == 0 {
		fmt.Fprintf(&b, "now: %s\n", price(r.Price, r.Currency))
	}
	fmt.Fprintf(&b, "%s\n", r.URL)

	if len(r.Elsewhere) > 0 {
		b.WriteString("\nOther shops\n")
		for _, o := range r.Elsewhere {
			unsure := ""
			if o.Doubt != "" {
				unsure = " (price unsure)"
			}
			fmt.Fprintf(&b, "%s: %s%s\n%s\n", o.Shop, price(o.Price, r.Currency), unsure, o.URL)
		}
	}
	return strings.TrimSpace(b.String())
}

// trimShop drops a shop name that page titles add, "Ryzen 7 - Jimms.fi" becomes "Ryzen 7"
func trimShop(name, host string) string {
	brand := strings.Split(host, ".")[0]
	for _, sep := range []string{" - ", " | "} {
		if i := strings.LastIndex(name, sep); i > 0 && strings.Contains(strings.ToLower(name[i:]), brand) {
			return name[:i]
		}
	}
	return name
}

// day turns 2026-08-16 into 16 Aug
func day(date string) string {
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return date
	}
	return t.Format("2 Jan")
}

func shop(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

func price(p float64, currency string) string {
	if currency == "EUR" {
		return fmt.Sprintf("€%.2f", p)
	}
	return fmt.Sprintf("%.2f %s", p, currency)
}
