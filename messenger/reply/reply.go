package reply

import (
	"fmt"
	"strings"

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

func Sources(sources []check.Source) string {
	if len(sources) == 0 {
		return "I don't have any links yet. Send me a product first!"
	}

	var b strings.Builder
	for _, s := range sources {
		fmt.Fprintf(&b, "%s\n%s\n\n", s.Label, s.URL)
	}
	return strings.TrimSpace(b.String())
}

func price(p float64, currency string) string {
	if currency == "EUR" {
		return fmt.Sprintf("€%.2f", p)
	}
	return fmt.Sprintf("%.2f %s", p, currency)
}
