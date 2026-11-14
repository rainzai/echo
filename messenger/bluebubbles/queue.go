package bluebubbles

// a chat's key in inboxes exists while its worker is running
func (s *Service) enqueue(m Message) {
	chat := m.Chats[0].GUID

	s.mu.Lock()
	_, running := s.inboxes[chat]
	s.inboxes[chat] = append(s.inboxes[chat], m)
	s.mu.Unlock()

	if !running {
		go s.work(chat)
	}
}

func (s *Service) work(chat string) {
	for {
		s.mu.Lock()
		msgs := s.inboxes[chat]
		if len(msgs) == 0 {
			delete(s.inboxes, chat)
			s.mu.Unlock()
			return
		}
		s.inboxes[chat] = nil
		s.mu.Unlock()

		s.reply(chat, msgs)
	}
}
