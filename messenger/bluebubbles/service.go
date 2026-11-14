package bluebubbles

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

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
	mu      sync.Mutex
	seen    map[string]struct{}
	inboxes map[string][]Message
	sources map[string][]check.Source
}

func NewService(client *Client, checker Checker) *Service {
	return &Service{
		client:  client,
		checker: checker,
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

var urlPattern = regexp.MustCompile(`https?://\S+`)

func (s *Service) reply(chat string, msgs []Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, m := range msgs {
		if strings.EqualFold(strings.TrimSpace(m.Text), "sources") {
			s.mu.Lock()
			text := reply.Sources(s.sources[chat])
			s.mu.Unlock()
			if err := s.client.SendText(ctx, chat, text); err != nil {
				log.Printf("reply failed guid=%s: %v", m.GUID, err)
				return
			}
			continue
		}

		url := urlPattern.FindString(m.Text)
		if url == "" {
			if err := s.client.SendText(ctx, chat, m.Text); err != nil {
				log.Printf("reply failed guid=%s: %v", m.GUID, err)
				return
			}
			log.Printf("replied guid=%s", m.GUID)
			continue
		}

		if err := s.client.React(ctx, chat, m.GUID, "like"); err != nil {
			log.Printf("react failed guid=%s: %v", m.GUID, err)
		}
		go s.check(chat, check.Request{Query: m.Text, Country: country(m.Handle.Address)})
	}
}

func (s *Service) check(chat string, req check.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	stop := s.keepTyping(ctx, chat)
	text := "Sorry, I couldn't check that right now. Try again in a bit."
	res, err := s.checker.Check(ctx, req)
	stop()
	switch {
	case errors.Is(err, check.ErrNoPrice):
		text = "I couldn't find a price for that. Try sending the product name or another shop's link."
	case err != nil:
		log.Printf("check failed query=%q: %v", req.Query, err)
	default:
		text = reply.Verdict(res)
		s.mu.Lock()
		s.sources[chat] = res.Sources
		s.mu.Unlock()
		log.Printf("checked query=%q fake_sale=%t", req.Query, res.FakeSale)
	}

	if err := s.client.Typing(ctx, chat, false); err != nil {
		log.Printf("typing failed chat=%s: %v", chat, err)
	}
	if err := s.client.SendText(ctx, chat, text); err != nil {
		log.Printf("verdict reply failed chat=%s: %v", chat, err)
	}
}

// keepTyping waits for the tapback to land (it clears the bubble), then toggles
// typing off and on so iMessage gets a fresh signal each round
func (s *Service) keepTyping(ctx context.Context, chat string) func() {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		wait := 1500 * time.Millisecond
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
