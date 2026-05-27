package control

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

func Call(socketPath string, req Request) (Response, error) {
	if socketPath == "" {
		return Response{}, fmt.Errorf("control socket path required")
	}
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return Response{}, fmt.Errorf("connect to Zen control socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return Response{}, fmt.Errorf("send control request: %w", err)
	}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("read control response: %w", err)
	}
	return resp, nil
}

