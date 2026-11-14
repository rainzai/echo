package check

type Request struct {
	Message      string  `json:"message"`
	URL          string  `json:"url,omitempty"`
	Product      string  `json:"product,omitempty"`
	Country      string  `json:"country,omitempty"`
	ClaimedPrice float64 `json:"claimed_price,omitempty"`
}

type Verdict string

const (
	FakeSale Verdict = "fake_sale"
	GoodDeal Verdict = "good_deal"
	Wait     Verdict = "wait"
	Unknown  Verdict = "unknown"
)

type Result struct {
	Verdict          Verdict      `json:"verdict"`
	Summary          string       `json:"summary"`
	PriceNow         float64      `json:"price_now"`
	Currency         string       `json:"currency"`
	PriceHistory     []PricePoint `json:"price_history"`
	CheaperElsewhere []Offer      `json:"cheaper_elsewhere"`
}

type PricePoint struct {
	Date   string  `json:"date"`
	Price  float64 `json:"price"`
	Source string  `json:"source"`
}

type Offer struct {
	Shop  string  `json:"shop"`
	Price float64 `json:"price"`
	URL   string  `json:"url"`
}
