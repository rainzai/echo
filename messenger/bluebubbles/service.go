package bluebubbles

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"messenger/agent"
	"messenger/check"
	"messenger/reply"
)

type Message struct {
	GUID     string `json:"guid"`
	Text     string `json:"text"`
	IsFromMe bool   `json:"isFromMe"`
	Handle   struct {
		Address string `json:"address"`
	} `json:"handle"`
	Chats []struct {
		GUID string `json:"guid"`
	} `json:"chats"`
}

type Checker interface {
	Check(ctx context.Context, req check.Request) (check.Result, error)
}

type Service struct {
	client  *Client
	checker Checker
	agent   *agent.Agent
	mu      sync.Mutex
	seen    map[string]struct{}
	inboxes map[string][]Message
	sources map[string][]check.Source
}

func NewService(client *Client, checker Checker, agent *agent.Agent) *Service {
	return &Service{
		client:  client,
		checker: checker,
		agent:   agent,
		seen:    make(map[string]struct{}),
		inboxes: make(map[string][]Message),
		sources: make(map[string][]check.Source),
	}
}

func (s *Service) HandleMessage(m Message, via string) {
	if m.IsFromMe || len(m.Chats) == 0 {
		return
	}

	// group chats are ";+;", 1:1 chats are ";-;"
	if strings.Contains(m.Chats[0].GUID, ";+;") {
		return
	}

	if !s.markSeen(m.GUID) {
		return
	}

	// attachments and app bubbles show up as U+FFFC in the text
	m.Text = strings.TrimSpace(strings.ReplaceAll(m.Text, "￼", ""))
	if m.Text == "" {
		return
	}

	log.Printf("message guid=%s via=%s chat=%s text=%q", m.GUID, via, m.Chats[0].GUID, m.Text)
	s.enqueue(m)
}

// Poll catches messages the webhook misses. BlueBubbles watches chat.db for
// changes and sometimes holds a message until the next one arrives.
func (s *Service) Poll(ctx context.Context) {
	start := time.Now()
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}

		after := time.Now().Add(-time.Minute)
		if after.Before(start) {
			after = start
		}
		pollCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		msgs, err := s.client.Messages(pollCtx, after)
		cancel()
		if err != nil {
			log.Printf("poll failed: %v", err)
			continue
		}

		for _, m := range msgs {
			s.HandleMessage(m, "poll")
		}
	}
}

func (s *Service) reply(chat string, msgs []Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// texts that arrived together get one answer
	var pending []Message
	for _, m := range msgs {
		if !strings.EqualFold(strings.TrimSpace(m.Text), "sources") {
			pending = append(pending, m)
			continue
		}
		if !s.talk(ctx, chat, pending) {
			return
		}
		pending = nil
		s.mu.Lock()
		text := reply.Sources(s.sources[chat])
		s.mu.Unlock()
		if err := s.client.SendText(ctx, chat, text); err != nil {
			log.Printf("reply failed guid=%s: %v", m.GUID, err)
			return
		}
	}
	s.talk(ctx, chat, pending)
}

func (s *Service) talk(ctx context.Context, chat string, msgs []Message) bool {
	if len(msgs) == 0 {
		return true
	}
	last := msgs[len(msgs)-1]
	texts := make([]string, len(msgs))
	for i, m := range msgs {
		texts[i] = m.Text
	}

	stop := s.keepTyping(ctx, chat, 0)
	defer func() { stop() }()

	var checked *check.Result
	text, err := s.agent.Reply(ctx, agent.Turn{
		Chat:    chat,
		Text:    strings.Join(texts, "\n"),
		Country: country(last.Handle.Address),
		Check: func(ctx context.Context, req check.Request) (check.Result, error) {
			// the tapback clears the typing bubble, so start it again after
			stop()
			if err := s.client.React(ctx, chat, last.GUID, "like"); err != nil {
				log.Printf("react failed guid=%s: %v", last.GUID, err)
			}
			stop = s.keepTyping(ctx, chat, 1500*time.Millisecond)

			res, err := s.checker.Check(ctx, req)
			switch {
			case errors.Is(err, check.ErrNoPrice):
				log.Printf("no price query=%q", req.Query)
			case err != nil:
				log.Printf("check failed query=%q: %v", req.Query, err)
			default:
				log.Printf("checked query=%q country=%s price=%.2f", req.Query, req.Country, res.Price)
				checked = &res
				s.mu.Lock()
				s.sources[chat] = res.Sources
				s.mu.Unlock()
			}
			return res, err
		},
	})
	stop()
	if err != nil {
		log.Printf("talk failed chat=%s: %v", chat, err)
		text = "Sorry, I lost my train of thought. Could you send that again?"
		if checked != nil {
			text = reply.Verdict(*checked)
		}
	}

	if err := s.client.Typing(ctx, chat, false); err != nil {
		log.Printf("typing failed chat=%s: %v", chat, err)
	}
	if err := s.client.SendText(ctx, chat, text); err != nil {
		log.Printf("reply failed chat=%s: %v", chat, err)
		return false
	}
	log.Printf("replied chat=%s", chat)
	return true
}

// keepTyping waits first (after a tapback, so it doesn't clear the bubble), then
// toggles typing off and on so iMessage gets a fresh signal each round
func (s *Service) keepTyping(ctx context.Context, chat string, first time.Duration) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		wait := first
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			_ = s.client.Typing(ctx, chat, false)
			if err := s.client.Typing(ctx, chat, true); err != nil && ctx.Err() == nil {
				log.Printf("typing failed chat=%s: %v", chat, err)
			}
			wait = 4 * time.Second
		}
	}()
	return cancel
}

// country guesses from the phone prefix, only used when nothing better is known
func country(address string) string {
	for prefix, code := range map[string]string{
		"+31": "NL", "+32": "BE", "+33": "FR", "+44": "GB", "+49": "DE", "+358": "FI", "+1": "US",
	} {
		if strings.HasPrefix(address, prefix) {
			return code
		}
	}
	return ""
}

func (s *Service) markSeen(guid string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.seen[guid]; ok {
		return false
	}
	s.seen[guid] = struct{}{}
	return true
}
