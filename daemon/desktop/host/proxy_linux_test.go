package host

import (
	"bufio"
	"encoding/binary"
	"net"
	"os"
	"testing"
	"time"

	"github.com/daoleno/zen/daemon/desktop"
	"golang.org/x/sys/unix"
)

func TestBrokerProxyRetiresMediaAndInputBeforeAgentCleanup(t *testing.T) {
	owner, _ := localPair(t)
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	agentFile := os.NewFile(uintptr(fds[0]), "test-agent")
	sourceFile := os.NewFile(uintptr(fds[1]), "test-source")
	source, err := net.FileConn(sourceFile)
	sourceFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	b := &broker{owner: owner, generation: 1}
	b.mu.Lock()
	mediaFile, err := b.proxyLocked(owner, agentFile)
	agentFile.Close()
	b.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		b.mu.Lock()
		b.retireLocked()
		b.mu.Unlock()
		b.proxyWorkers.Wait()
	}()
	media, err := net.FileConn(mediaFile)
	mediaFile.Close()
	if err != nil {
		t.Fatal(err)
	}
	defer media.Close()
	_ = source.SetDeadline(time.Now().Add(2 * time.Second))
	_ = media.SetDeadline(time.Now().Add(2 * time.Second))
	frame := []byte{0, 0, 0, 1, 0x65, 1, 2, 3}
	header := make([]byte, 5)
	binary.BigEndian.PutUint32(header, uint32(len(frame)+1))
	header[4] = 2
	if _, err := source.Write(append(header, frame...)); err != nil {
		t.Fatal(err)
	}
	kind, got, err := desktop.ReadPacket(media)
	if err != nil || kind != 2 || string(got) != string(frame) {
		t.Fatal("proxy did not carry the agent packet")
	}
	line := "release 0 0 0 false 0\n"
	if _, err := media.Write([]byte(line)); err != nil {
		t.Fatal(err)
	}
	if got, err := bufio.NewReader(source).ReadString('\n'); err != nil || got != line {
		t.Fatal("proxy did not carry the admitted input")
	}
	b.mu.Lock()
	b.retireLocked()
	b.mu.Unlock()
	b.proxyWorkers.Wait()
	if _, err := media.Write([]byte(line)); err == nil {
		t.Fatal("retired input reached the proxy")
	}
	if _, err := source.Write(frame); err == nil {
		t.Fatal("retired media reached the proxy")
	}
	if b.owner != nil || b.media != nil || b.agent != nil {
		t.Fatal("retired capabilities remain published")
	}
}
