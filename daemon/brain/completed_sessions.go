package brain

import "github.com/daoleno/zen/daemon/watcher"

// CompletedOwnedTurns is a derived cleanup set, not another durable queue.
// Only the latest exact delegated result of an explicitly closed Work qualifies.
func (s *Store) CompletedOwnedTurns(workID string) ([]watcher.TurnSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	database, err := s.loadPresentationLocked()
	if err != nil {
		return nil, err
	}
	busy := map[string]bool{}
	for _, st := range s.fsm.ListViews() {
		if st.Attempt != nil {
			busy[st.Attempt.SessionID] = true
		}
		if admission := st.ActiveAdmission(); admission != nil {
			busy[admission.SessionID] = true
		}
	}
	var result []watcher.TurnSnapshot
	for _, turn := range database.BrainTurns {
		if workID != "" && turn.WorkID != workID {
			continue
		}
		if busy[turn.SessionID] || isHostHandlingTurn(turn) || !turn.SignalProtocol ||
			(turn.Status != watcher.TurnDone && turn.Status != watcher.TurnFailed) {
			continue
		}
		st, err := s.fsmState(turn.WorkID)
		if err != nil {
			return nil, err
		}
		if !st.Status.Terminal() {
			continue
		}
		current, found := currentTurnForSession(database, turn.SessionID)
		if found && current.TurnID == turn.TurnID {
			result = append(result, turn.snapshot())
		}
	}
	return result, nil
}
