package bluebubbles

import (
	"context"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"messenger/check"
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
}

func NewService(client *Client, checker Checker) *Service {
	return &Service{
		client:  client,
		checker: checker,
		seen:    make(map[string]struct{}),
		inboxes: make(map[string][]Message),
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
		url := urlPattern.FindString(m.Text)
		if url == "" {
			if err := s.client.SendText(ctx, chat, m.Text); err != nil {
				log.Printf("reply failed guid=%s: %v", m.GUID, err)
				return
			}
			log.Printf("replied guid=%s", m.GUID)
			continue
		}

		if err := s.client.Typing(ctx, chat, true); err != nil {
			log.Printf("typing failed chat=%s: %v", chat, err)
		}
		if err := s.client.React(ctx, chat, m.GUID, "like"); err != nil {
			log.Printf("react failed guid=%s: %v", m.GUID, err)
		}
		go s.check(chat, check.Request{Message: m.Text, URL: url})
	}
}

func (s *Service) check(chat string, req check.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	text := "Sorry, I couldn't check that right now. Try again in a bit."
	res, err := s.checker.Check(ctx, req)
	if err != nil {
		log.Printf("check failed url=%s: %v", req.URL, err)
	} else {
		text = res.Summary
		log.Printf("checked url=%s verdict=%s", req.URL, res.Verdict)
	}

	if err := s.client.Typing(ctx, chat, false); err != nil {
		log.Printf("typing failed chat=%s: %v", chat, err)
	}
	if err := s.client.SendText(ctx, chat, text); err != nil {
		log.Printf("verdict reply failed url=%s: %v", req.URL, err)
	}
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
