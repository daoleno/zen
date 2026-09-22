package work

import (
	"context"
	"fmt"
	"golang.org/x/term"
	"os"
	"time"
)

func runDSHTerminal(ctx context.Context, id string, exited chan error) error {
	if term.IsTerminal(int(os.Stdin.Fd())) {
		state, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return err
		}
		defer term.Restore(int(os.Stdin.Fd()), state)
	}
	fmt.Fprint(os.Stdout, "\x1b[?2004h\r\n› ")
	defer fmt.Fprint(os.Stdout, "\x1b[?2004l")
	input := make(chan byte, 128)
	go func() {
		defer close(input)
		buffer := make([]byte, 1)
		for {
			n, err := os.Stdin.Read(buffer)
			if n > 0 {
				select {
				case input <- buffer[0]:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	var draft []byte
	var escape []byte
	pasted := false
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-exited:
			exited <- err
			return fmt.Errorf("DSH native runtime exited: %v", err)
		case value, ok := <-input:
			if !ok {
				return nil
			}
			if len(escape) > 0 || value == 27 {
				escape = append(escape, value)
				if string(escape) == "\x1b[200~" {
					pasted = true
					escape = nil
				} else if string(escape) == "\x1b[201~" {
					pasted = false
					escape = nil
				} else if len(escape) >= 6 {
					escape = nil
				}
				continue
			}
			if value == 3 {
				requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				err := CallDSH(requestCtx, id, "session.cancel", map[string]any{}, nil)
				cancel()
				if err != nil {
					fmt.Fprintf(os.Stdout, "\r\nStop failed: %v\r\n› ", err)
				} else {
					fmt.Fprint(os.Stdout, "\r\nStop requested.\r\n› ")
				}
				draft = nil
				continue
			}
			if (value == '\r' || value == '\n') && !pasted {
				if len(draft) == 0 {
					continue
				}
				text := string(draft)
				draft = nil
				requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				content, err := dshPromptContent(text)
				if err == nil {
					err = CallDSH(requestCtx, id, "session.prompt", map[string]any{"mode": "queue", "content": content}, nil)
				}
				cancel()
				if err != nil {
					fmt.Fprintf(os.Stdout, "\r\nDSH: %v\r\n", err)
				} else {
					fmt.Fprint(os.Stdout, "\r\nSubmitted to DSH. Read replies in Interface.\r\n")
				}
				fmt.Fprint(os.Stdout, "› ")
			} else if value == 127 || value == 8 {
				if len(draft) > 0 {
					draft = draft[:len(draft)-1]
					fmt.Fprint(os.Stdout, "\b \b")
				}
			} else if len(draft) < 4<<20 {
				draft = append(draft, value)
				_, _ = os.Stdout.Write([]byte{value})
			}
		}
	}
}
