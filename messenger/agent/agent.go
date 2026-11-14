package agent

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"golang.org/x/text/currency"
	"golang.org/x/text/language"

	"messenger/check"
)

// keeps the last few turns per chat, lost on restart
const maxHistory = 30

// a turn can ask for a few checks before it answers
const maxCalls = 3

const (
	RoleUser = "user"
	RoleEcho = "echo"
	RoleTool = "tool"
)

type Message struct {
	Role   string
	Text   string
	Call   *Call   // echo asks to run a tool
	Result *Result // what the tool returned
}

type Call struct {
	Name string
	Args map[string]any
	// Signature is opaque provider state that has to be sent back with the call
	Signature string
}

type Result struct {
	Name string
	Data map[string]any
}

type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type Model interface {
	Generate(ctx context.Context, system string, tools []Tool, history []Message) (Message, error)
}

// Turn is one round of messages from a chat
type Turn struct {
	Chat    string
	Text    string
	Country string // from the phone number, empty if unknown
	Images  [][]byte
	Check   func(ctx context.Context, req check.Request) (check.Result, error)
}

type Agent struct {
	model   Model
	vision  *Vision
	mu      sync.Mutex
	history map[string][]Message
}

func New(model Model, vision *Vision) *Agent {
	return &Agent{model: model, vision: vision, history: make(map[string][]Message)}
}

func (a *Agent) Reply(ctx context.Context, t Turn) (string, error) {
	// the vision model reads photos first, its answer becomes part of the message
	for _, img := range t.Images {
		seen, err := a.vision.Look(ctx, img)
		if err != nil {
			log.Printf("vision failed chat=%s: %v", t.Chat, err)
			seen = `{"error": "couldn't read the photo"}`
		}
		log.Printf("vision chat=%s: %s", t.Chat, seen)
		t.Text = strings.TrimSpace(t.Text + "\n[photo: " + seen + "]")
	}

	a.mu.Lock()
	history := append(append([]Message{}, a.history[t.Chat]...), Message{Role: RoleUser, Text: t.Text})
	a.mu.Unlock()

	system := systemPrompt
	if t.Country != "" {
		system += "\nThe user's phone number is from " + t.Country + ". Check prices for that country unless they say otherwise, and don't ask."
	}

	for range maxCalls + 1 {
		m, err := a.model.Generate(ctx, system, tools, history)
		if err != nil {
			return "", err
		}
		history = append(history, m)

		if m.Call == nil {
			a.mu.Lock()
			a.history[t.Chat] = trim(history)
			a.mu.Unlock()
			return m.Text, nil
		}

		history = append(history, Message{Role: RoleTool, Result: &Result{Name: m.Call.Name, Data: a.run(ctx, t, m.Call)}})
	}
	return "", errors.New("too many tool calls")
}

func (a *Agent) run(ctx context.Context, t Turn, c *Call) map[string]any {
	if c.Name != "check_deal" {
		return map[string]any{"error": fmt.Sprintf("unknown tool %q", c.Name)}
	}

	req := check.Request{Country: t.Country}
	req.Query, _ = c.Args["query"].(string)
	if country, _ := c.Args["country"].(string); country != "" {
		req.Country = country
	}
	req.URL, _ = c.Args["url"].(string)
	req.Shop, _ = c.Args["shop"].(string)
	req.Condition, _ = c.Args["condition"].(string)
	req.BuyWord, _ = c.Args["buy_word"].(string)
	if region, err := language.ParseRegion(req.Country); err == nil {
		if cur, ok := currency.FromRegion(region); ok {
			req.Currency = cur.String()
		}
	}

	res, err := t.Check(ctx, req)
	switch {
	case errors.Is(err, check.ErrNoPrice):
		return map[string]any{"error": "no price found for this product"}
	case err != nil:
		return map[string]any{"error": "the price check failed"}
	}
	return facts(req, res)
}

// trim never cuts a tool call off from its result
func trim(history []Message) []Message {
	if len(history) <= maxHistory {
		return history
	}
	history = history[len(history)-maxHistory:]
	for len(history) > 0 && (history[0].Role != RoleUser || history[0].Result != nil) {
		history = history[1:]
	}
	return history
}
