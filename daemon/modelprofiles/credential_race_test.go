package modelprofiles

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// barrierCredStore pauses Set/Delete after signaling entry so Owner.mu
// serialization schedules can be forced deterministically.
type barrierCredStore struct {
	inner      *MemoryCredentialStore
	mu         sync.Mutex
	setEnter   chan struct{}
	setRelease chan struct{}
	delEnter   chan struct{}
	delRelease chan struct{}
}

func newBarrierCredStore() *barrierCredStore {
	return &barrierCredStore{inner: NewMemoryCredentialStore()}
}

func (b *barrierCredStore) Available() bool { return b.inner.Available() }

func (b *barrierCredStore) Get(ref string) (string, bool, error) {
	return b.inner.Get(ref)
}

func (b *barrierCredStore) Set(ref, secret string) error {
	b.mu.Lock()
	enter, release := b.setEnter, b.setRelease
	b.mu.Unlock()
	if enter != nil {
		select {
		case <-enter:
		default:
			close(enter)
		}
		if release != nil {
			<-release
		}
	}
	return b.inner.Set(ref, secret)
}

func (b *barrierCredStore) Delete(ref string) error {
	b.mu.Lock()
	enter, release := b.delEnter, b.delRelease
	b.mu.Unlock()
	if enter != nil {
		select {
		case <-enter:
		default:
			close(enter)
		}
		if release != nil {
			<-release
		}
	}
	return b.inner.Delete(ref)
}

func (b *barrierCredStore) SnapshotRefs() []string { return b.inner.SnapshotRefs() }

func (b *barrierCredStore) armSet() (entered, release chan struct{}) {
	entered = make(chan struct{})
	release = make(chan struct{})
	b.mu.Lock()
	b.setEnter, b.setRelease = entered, release
	b.mu.Unlock()
	return entered, release
}

func (b *barrierCredStore) armDel() (entered, release chan struct{}) {
	entered = make(chan struct{})
	release = make(chan struct{})
	b.mu.Lock()
	b.delEnter, b.delRelease = entered, release
	b.mu.Unlock()
	return entered, release
}

func (b *barrierCredStore) disarm() {
	b.mu.Lock()
	b.setEnter, b.setRelease, b.delEnter, b.delRelease = nil, nil, nil, nil
	b.mu.Unlock()
}

func assertNoOrphanKey(t *testing.T, owner *Owner, store *barrierCredStore, id string) {
	t.Helper()
	_, err := owner.GetProfile(id)
	absent := errors.Is(err, ErrNotFound)
	hasKey := false
	ref := CredentialRefFor(id)
	for _, r := range store.SnapshotRefs() {
		if r == ref {
			hasKey = true
			break
		}
	}
	if absent && hasKey {
		t.Fatalf("orphan key for absent connection %q refs=%v", id, store.SnapshotRefs())
	}
	if !absent && !hasKey {
		// Live connection without key is allowed (not ready); not an orphan.
		return
	}
}

func TestCredentialSetDeleteSerializationNoOrphan(t *testing.T) {
	t.Run("set_before_delete", func(t *testing.T) {
		owner := startTestOwner(t, func(string) (string, bool) { return "", false })
		store := newBarrierCredStore()
		owner.SetCredentialStore(store)

		conn, err := CompileProviderConnection(ProviderConnectionInput{PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.UpsertProfile(conn, 0, true); err != nil {
			t.Fatal(err)
		}
		rev := owner.Catalog().Revision
		entered, release := store.armSet()

		setErr := make(chan error, 1)
		go func() {
			_, err := owner.SetProviderCredential(conn.ID, "sk-race-set")
			setErr <- err
		}()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("set did not enter credential store")
		}

		delErr := make(chan error, 1)
		delStarted := make(chan struct{})
		go func() {
			close(delStarted)
			_, err := owner.DeleteProviderConnection(conn.ID, rev)
			delErr <- err
		}()
		<-delStarted
		select {
		case err := <-delErr:
			t.Fatalf("delete must block behind in-flight set; got %v", err)
		case <-time.After(50 * time.Millisecond):
		}

		close(release)
		if err := <-setErr; err != nil {
			t.Fatalf("set: %v", err)
		}
		if err := <-delErr; err != nil {
			t.Fatalf("delete: %v", err)
		}
		store.disarm()
		assertNoOrphanKey(t, owner, store, conn.ID)
		if _, err := owner.GetProfile(conn.ID); !errors.Is(err, ErrNotFound) {
			t.Fatal("connection must be gone after delete")
		}
		if len(store.SnapshotRefs()) != 0 {
			t.Fatalf("keys remain: %v", store.SnapshotRefs())
		}
	})

	t.Run("delete_before_set", func(t *testing.T) {
		owner := startTestOwner(t, func(string) (string, bool) { return "", false })
		store := newBarrierCredStore()
		owner.SetCredentialStore(store)

		conn, err := CompileProviderConnection(ProviderConnectionInput{Name: "RaceDel", PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.UpsertProfile(conn, 0, true); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.SetProviderCredential(conn.ID, "sk-existing"); err != nil {
			t.Fatal(err)
		}
		rev := owner.Catalog().Revision
		entered, release := store.armDel()

		delErr := make(chan error, 1)
		go func() {
			_, err := owner.DeleteProviderConnection(conn.ID, rev)
			delErr <- err
		}()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("delete did not enter credential store")
		}

		setErr := make(chan error, 1)
		setStarted := make(chan struct{})
		go func() {
			close(setStarted)
			_, err := owner.SetProviderCredential(conn.ID, "sk-after-delete")
			setErr <- err
		}()
		<-setStarted
		select {
		case err := <-setErr:
			t.Fatalf("set must block behind in-flight delete; got %v", err)
		case <-time.After(50 * time.Millisecond):
		}

		close(release)
		if err := <-delErr; err != nil {
			t.Fatalf("delete: %v", err)
		}
		if err := <-setErr; !errors.Is(err, ErrNotFound) {
			t.Fatalf("set after delete want not-found, got %v", err)
		}
		store.disarm()
		assertNoOrphanKey(t, owner, store, conn.ID)
		if len(store.SnapshotRefs()) != 0 {
			t.Fatalf("orphan keys: %v", store.SnapshotRefs())
		}
	})

	t.Run("clear_before_delete", func(t *testing.T) {
		owner := startTestOwner(t, func(string) (string, bool) { return "", false })
		store := newBarrierCredStore()
		owner.SetCredentialStore(store)
		conn, err := CompileProviderConnection(ProviderConnectionInput{Name: "ClearDel", PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.UpsertProfile(conn, 0, true); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.SetProviderCredential(conn.ID, "sk-clear"); err != nil {
			t.Fatal(err)
		}
		rev := owner.Catalog().Revision
		entered, release := store.armDel()

		clearErr := make(chan error, 1)
		go func() {
			// First Clear will arm via Delete path — use del barrier for Clear's Delete call.
			_, err := owner.ClearProviderCredential(conn.ID)
			clearErr <- err
		}()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("clear did not enter credential store delete")
		}

		delErr := make(chan error, 1)
		go func() {
			_, err := owner.DeleteProviderConnection(conn.ID, rev)
			delErr <- err
		}()
		select {
		case err := <-delErr:
			t.Fatalf("delete must block behind clear; got %v", err)
		case <-time.After(50 * time.Millisecond):
		}

		close(release)
		if err := <-clearErr; err != nil {
			t.Fatalf("clear: %v", err)
		}
		// Delete's credential-store call may also hit the same barrier channels (already closed).
		// Disarm before delete proceeds further — release already closed; delete may be waiting on Owner.mu.
		store.disarm()
		if err := <-delErr; err != nil {
			t.Fatalf("delete: %v", err)
		}
		assertNoOrphanKey(t, owner, store, conn.ID)
		if len(store.SnapshotRefs()) != 0 {
			t.Fatalf("keys remain: %v", store.SnapshotRefs())
		}
	})

	t.Run("delete_before_clear", func(t *testing.T) {
		owner := startTestOwner(t, func(string) (string, bool) { return "", false })
		store := newBarrierCredStore()
		owner.SetCredentialStore(store)
		conn, err := CompileProviderConnection(ProviderConnectionInput{Name: "DelClear", PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.UpsertProfile(conn, 0, true); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.SetProviderCredential(conn.ID, "sk-dc"); err != nil {
			t.Fatal(err)
		}
		rev := owner.Catalog().Revision
		entered, release := store.armDel()

		delErr := make(chan error, 1)
		go func() {
			_, err := owner.DeleteProviderConnection(conn.ID, rev)
			delErr <- err
		}()
		<-entered

		clearErr := make(chan error, 1)
		go func() {
			_, err := owner.ClearProviderCredential(conn.ID)
			clearErr <- err
		}()
		select {
		case err := <-clearErr:
			t.Fatalf("clear must block behind delete; got %v", err)
		case <-time.After(50 * time.Millisecond):
		}

		close(release)
		if err := <-delErr; err != nil {
			t.Fatalf("delete: %v", err)
		}
		store.disarm()
		if err := <-clearErr; !errors.Is(err, ErrNotFound) {
			t.Fatalf("clear after delete want not-found, got %v", err)
		}
		assertNoOrphanKey(t, owner, store, conn.ID)
		if len(store.SnapshotRefs()) != 0 {
			t.Fatalf("orphan keys: %v", store.SnapshotRefs())
		}
	})

	t.Run("set_credential_store_serialized", func(t *testing.T) {
		owner := startTestOwner(t, func(string) (string, bool) { return "", false })
		store := newBarrierCredStore()
		owner.SetCredentialStore(store)
		conn, err := CompileProviderConnection(ProviderConnectionInput{Name: "StoreSwap", PresetID: ProviderPresetDeepSeek, Client: ClientCodex})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := owner.UpsertProfile(conn, 0, true); err != nil {
			t.Fatal(err)
		}
		entered, release := store.armSet()
		setErr := make(chan error, 1)
		go func() {
			_, err := owner.SetProviderCredential(conn.ID, "sk-swap")
			setErr <- err
		}()
		<-entered

		replaced := make(chan struct{})
		go func() {
			owner.SetCredentialStore(NewMemoryCredentialStore())
			close(replaced)
		}()
		select {
		case <-replaced:
			t.Fatal("SetCredentialStore must wait for in-flight Set")
		case <-time.After(50 * time.Millisecond):
		}
		close(release)
		if err := <-setErr; err != nil {
			t.Fatalf("set: %v", err)
		}
		<-replaced
		store.disarm()
		// Original store retained the key; replacement is empty — connection still present.
		if refs := store.SnapshotRefs(); len(refs) == 0 {
			t.Fatal("in-flight set must complete on the store that started it")
		}
	})
}
