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
	client *Client
	mu     sync.Mutex
	seen   map[string]struct{}
}

func NewService(client *Client) *Service {
	return &Service{client: client, seen: make(map[string]struct{})}
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

	go s.process(m, via)
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

func (s *Service) process(m Message, via string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	chat := m.Chats[0].GUID
	log.Printf("message guid=%s via=%s chat=%s text=%q", m.GUID, via, chat, m.Text)

	if m.Text == "" {
		return
	}

	if err := s.client.SendText(ctx, chat, m.Text); err != nil {
		log.Printf("reply failed guid=%s: %v", m.GUID, err)
		return
	}
	log.Printf("replied guid=%s", m.GUID)
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
