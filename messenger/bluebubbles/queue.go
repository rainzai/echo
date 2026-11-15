package bluebubbles

import "time"

// a chat's key in inboxes exists while its worker is running
func (s *Service) enqueue(m Message) {
	chat := m.Chats[0].GUID

	s.mu.Lock()
	_, running := s.inboxes[chat]
	s.inboxes[chat] = append(s.inboxes[chat], m)
	s.lastIn[chat] = time.Now()
	s.mu.Unlock()

	if !running {
		go s.work(chat)
	}
}

// people send a question in pieces, and a mac splits a link from its text,
// so answer once the chat has been quiet for a moment
const (
	quiet   = 3 * time.Second
	maxWait = 8 * time.Second
)

func (s *Service) work(chat string) {
	for {
		s.settle(chat)
		s.mu.Lock()
		msgs := s.inboxes[chat]
		if len(msgs) == 0 {
			delete(s.inboxes, chat)
			delete(s.lastIn, chat)
			s.mu.Unlock()
			return
		}
		s.inboxes[chat] = nil
		s.mu.Unlock()

		s.reply(chat, msgs)
	}
}

func (s *Service) settle(chat string) {
	start := time.Now()
	for time.Since(start) < maxWait {
		s.mu.Lock()
		since := time.Since(s.lastIn[chat])
		s.mu.Unlock()
		if since >= quiet {
			return
		}
		time.Sleep(quiet - since)
	}
}
