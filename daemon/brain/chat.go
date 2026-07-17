package brain

import (
	"fmt"
	"strings"
	"time"
)

func (s *Service) ChatThreadID() (string, error) {
	if s == nil || s.store == nil {
		return "", nil
	}
	return s.store.ChatThreadID()
}

func (s *Service) HasChatThread(threadID string) (bool, error) {
	if s == nil || s.store == nil {
		return false, nil
	}
	return s.store.HasChatThread(threadID)
}

func (s *Service) ChatThreadIDs() ([]string, error) {
	if s == nil || s.store == nil {
		return []string{}, nil
	}
	return s.store.ChatThreadIDs()
}

func (s *Service) NewChat() (Snapshot, error) {
	if s == nil || s.store == nil || s.watcher == nil {
		return Snapshot{}, fmt.Errorf("brain service is not configured")
	}
	executor := s.hostExecutor()
	if hostSession, err := s.store.HostSession(); err == nil {
		if id := strings.TrimSpace(hostSession.ID); id != "" && s.watcher.HasSession(id) {
			if err := s.watcher.KillSession(id); err != nil {
				return Snapshot{}, err
			}
		}
	} else {
		return Snapshot{}, err
	}
	if err := s.store.SetHostSession("", executor.ID); err != nil {
		return Snapshot{}, err
	}
	if _, err := s.ensureHostAgent(executor); err != nil {
		return Snapshot{}, err
	}
	threadID := newChatThreadID()
	if err := s.store.SetChatState(ChatState{ThreadID: threadID}); err != nil {
		return Snapshot{}, err
	}
	return s.Snapshot()
}

func (s *Service) nowUTC() time.Time {
	if s != nil && s.now != nil {
		return s.now().UTC()
	}
	return time.Now().UTC()
}
