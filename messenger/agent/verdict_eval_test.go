//go:build eval

// run: go test -tags eval ./agent -run Verdict -v -count=1 [-runs 5]
package agent

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"messenger/check"
)

var runs = flag.Int("runs", 3, "runs per case")

type evalResult struct {
	NoPrice bool `json:"no_price"`
	Fail    bool `json:"error"`
	check.Result
}

type verdictCase struct {
	Name     string       `json:"name"`
	Kind     string       `json:"kind"`
	Country  string       `json:"country"`
	Messages []string     `json:"messages"`
	Results  []evalResult `json:"results"`
	Expect   struct {
		Call         *bool             `json:"call"`
		Args         map[string]string `json:"args"`
		Verdict      string            `json:"verdict"`
		Mention      []string          `json:"mention"`
		Forbid       []string          `json:"forbid"`
		Lang         string            `json:"lang"`
		MaxSentences int               `json:"max_sentences"`
	} `json:"expect"`
}

var (
	links    = regexp.MustCompile(`(?i)https?://|www\.|\b[a-z0-9-]+\.(nl|com|de|fi|co\.uk)\b`)
	numbers  = regexp.MustCompile(`\d[\d.,]*\d|\d`)
	sentence = regexp.MustCompile(`[.!?](\s|$)`)
)

var stopwords = map[string][]string{
	"en": {"the", "it", "and", "for", "at", "but", "you", "this", "that", "price", "if", "to"},
	"nl": {"de", "het", "een", "niet", "voor", "bij", "en", "je", "dit", "dat", "prijs", "als", "van"},
	"fi": {"on", "ei", "ja", "se", "hinta", "että", "tämä", "mutta", "kuin", "hyvä", "oli", "tai"},
}

func lang(text string) string {
	words := strings.Fields(strings.ToLower(regexp.MustCompile(`[^\pL\s]`).ReplaceAllString(text, " ")))
	best, most := "", 0
	for l, sw := range stopwords {
		n := 0
		for _, w := range words {
			for _, s := range sw {
				if w == s {
					n++
				}
			}
		}
		if n > most {
			best, most = l, n
		}
	}
	return best
}

// allowed collects every number the reply may use: from the facts, the user and the product names
func allowed(texts []string, facts []map[string]any) []float64 {
	var out []float64
	var walk func(v any)
	walk = func(v any) {
		switch v := v.(type) {
		case float64:
			out = append(out, v)
		case string:
			texts = append(texts, v)
		case map[string]any:
			for _, x := range v {
				walk(x)
			}
		case []map[string]any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	for _, f := range facts {
		walk(f)
	}
	for _, t := range texts {
		for _, n := range numbers.FindAllString(t, -1) {
			out = append(out, parse(n)...)
		}
	}
	return append(out, 30, 1)
}

// parse reads 1.039 / 1,039.00 / 57,99 / 57.99 every way it could be meant
func parse(s string) []float64 {
	var out []float64
	for _, c := range []string{
		strings.ReplaceAll(s, ",", ""),
		strings.ReplaceAll(s, ".", ""),
		strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", "."),
		strings.ReplaceAll(s, ",", "."),
	} {
		if f, err := strconv.ParseFloat(strings.Trim(c, ".,"), 64); err == nil {
			out = append(out, f)
		}
	}
	return out
}

// madeUp skips numbers glued to letters, those are model names like XM5 or 256GB
func madeUp(reply string, ok []float64) []string {
	var bad []string
	for _, loc := range numbers.FindAllStringIndex(reply, -1) {
		n := reply[loc[0]:loc[1]]
		if letter(reply, loc[0]-1) || letter(reply, loc[1]) {
			continue
		}
		found := false
		for _, v := range parse(n) {
			for _, a := range ok {
				if math.Abs(v-a) < 0.011 || math.Abs(v-math.Round(a)) < 0.011 {
					found = true
				}
			}
		}
		if !found {
			bad = append(bad, n)
		}
	}
	return bad
}

const judgePrompt = `You read one reply from a deal checking assistant. Which verdict does it give about the price?
good: it's a good deal, a real discount or a real price drop
bad: it's not a deal, the discount isn't real, it was cheaper before, or they should wait
normal: it's the usual or normal price, nothing special
none: no verdict, e.g. it couldn't check, has no price history, asks a question or talks about something else
Answer with exactly one word: good, bad, normal or none.`

func verdict(ctx context.Context, judge Model, reply string) string {
	m, err := judge.Generate(ctx, judgePrompt, nil, []Message{{Role: RoleUser, Text: reply}})
	if err != nil {
		return "judge error"
	}
	return strings.Trim(strings.ToLower(strings.TrimSpace(m.Text)), ".")
}

func letter(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-'
}

func runCase(ctx context.Context, model, judge Model, c verdictCase, id int) (string, []string) {
	var calls []check.Request
	var given []map[string]any
	var mu sync.Mutex
	chk := func(ctx context.Context, req check.Request) (check.Result, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, req)
		if len(calls) > len(c.Results) {
			return check.Result{}, check.ErrNoPrice
		}
		r := c.Results[len(calls)-1]
		switch {
		case r.NoPrice:
			return check.Result{}, check.ErrNoPrice
		case r.Fail:
			return check.Result{}, fmt.Errorf("check failed")
		}
		given = append(given, facts(req, r.Result))
		return r.Result, nil
	}

	a := New(model, NewGemini(os.Getenv("GEMINI_API_KEY"), "gemini-3.8-flash", "low"), nil)
	chat := fmt.Sprintf("%s-%d", c.Name, id)
	var reply string
	for _, m := range c.Messages {
		r, err := a.Reply(ctx, Turn{Chat: chat, Text: m, Country: c.Country, Check: chk})
		if err != nil {
			return "", []string{"error " + err.Error()}
		}
		reply = r
	}

	var problems []string
	e := c.Expect
	if e.Call != nil && (len(calls) > 0) != *e.Call {
		problems = append(problems, fmt.Sprintf("called check %t", len(calls) > 0))
	}
	for field, pattern := range e.Args {
		re := regexp.MustCompile("(?i)" + pattern)
		hit := false
		for _, r := range calls {
			v := map[string]string{"query": r.Query, "country": r.Country, "url": r.URL, "shop": r.Shop, "condition": r.Condition}[field]
			hit = hit || re.MatchString(v)
		}
		if !hit {
			problems = append(problems, fmt.Sprintf("%s not %q in %+v", field, pattern, calls))
		}
	}

	if e.Verdict != "" {
		if v := verdict(ctx, judge, reply); !regexp.MustCompile("^(" + e.Verdict + ")$").MatchString(v) {
			problems = append(problems, "verdict "+v)
		}
	}
	for _, m := range e.Mention {
		if !regexp.MustCompile("(?i)" + m).MatchString(reply) {
			problems = append(problems, "doesn't mention "+m)
		}
	}
	for _, f := range e.Forbid {
		if regexp.MustCompile("(?i)" + f).MatchString(reply) {
			problems = append(problems, "mentions "+f)
		}
	}
	if e.Lang != "" && lang(reply) != e.Lang {
		problems = append(problems, "language "+lang(reply))
	}
	if links.MatchString(reply) {
		problems = append(problems, "link "+links.FindString(reply))
	}
	if bad := madeUp(reply, allowed(c.Messages, given)); len(bad) > 0 {
		problems = append(problems, "made up "+strings.Join(bad, " "))
	}
	max := e.MaxSentences
	if max == 0 {
		max = 3
	}
	if n := len(sentence.FindAllString(reply+" ", -1)); n > max {
		problems = append(problems, fmt.Sprintf("%d sentences", n))
	}
	return reply, problems
}

func TestVerdict(t *testing.T) {
	var cases []verdictCase
	b, err := os.ReadFile("../eval/verdict/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}

	model := NewGemini(os.Getenv("GEMINI_API_KEY"), envOr("GEMINI_MODEL", "gemini-4-hydrogen"), envOr("GEMINI_THINKING", "low"))
	// a different model judges, so hydrogen doesn't grade itself
	judge := NewGemini(os.Getenv("GEMINI_API_KEY"), envOr("JUDGE_MODEL", "gemini-3.8-flash"), "low")
	type outcome struct {
		reply    string
		problems []string
	}
	results := make([][]outcome, len(cases))
	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i, c := range cases {
		results[i] = make([]outcome, *runs)
		for r := range *runs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				reply, problems := runCase(context.Background(), model, judge, c, r)
				results[i][r] = outcome{reply, problems}
			}()
		}
	}
	wg.Wait()

	total, right := 0, 0
	kinds := map[string][2]int{}
	for i, c := range cases {
		ok := 0
		seen := map[string]bool{}
		for _, o := range results[i] {
			if len(o.problems) == 0 {
				ok++
			}
			for _, p := range o.problems {
				seen[p] = true
			}
		}
		total += *runs
		right += ok
		k := kinds[c.Kind]
		kinds[c.Kind] = [2]int{k[0] + ok, k[1] + *runs}
		var ps []string
		for p := range seen {
			ps = append(ps, p)
		}
		sort.Strings(ps)
		mark := "OK  "
		if ok < *runs {
			mark = fmt.Sprintf("%d/%d ", ok, *runs)
		}
		fmt.Printf("%s %-28s %s\n", mark, c.Name, strings.Join(ps, "; "))
		show := results[i][0]
		for _, o := range results[i] {
			if len(o.problems) > 0 {
				show = o
				break
			}
		}
		fmt.Printf("      %s\n", strings.ReplaceAll(show.reply, "\n", " "))
	}
	fmt.Printf("\n%d/%d right\n", right, total)
	var names []string
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Printf("  %-15s %d/%d\n", k, kinds[k][0], kinds[k][1])
	}
}

func envOr(k, dv string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return dv
}
