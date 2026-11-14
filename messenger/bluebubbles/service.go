package bluebubbles

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
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

type Service struct {
	client  *Client
	mu      sync.Mutex
	seen    map[string]struct{}
	inboxes map[string][]Message
}

func NewService(client *Client) *Service {
	return &Service{
		client:  client,
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

func (s *Service) reply(chat string, msgs []Message) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, m := range msgs {
		if err := s.client.SendText(ctx, chat, m.Text); err != nil {
			log.Printf("reply failed guid=%s: %v", m.GUID, err)
			return
		}
		log.Printf("replied guid=%s", m.GUID)
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
