package control

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

type CallResult struct {
	Response              Response
	RequestMayHaveArrived bool
}

func Call(socketPath string, req Request) (Response, error) {
	return CallWithTimeout(socketPath, req, 30*time.Second)
}

func CallWithTimeout(
	socketPath string,
	req Request,
	timeout time.Duration,
) (Response, error) {
	result, err := CallWithTimeoutResult(socketPath, req, timeout)
	return result.Response, err
}

func CallWithTimeoutResult(
	socketPath string,
	req Request,
	timeout time.Duration,
) (CallResult, error) {
	if socketPath == "" {
		return CallResult{}, fmt.Errorf("control socket path required")
	}
	if timeout <= 0 {
		return CallResult{}, fmt.Errorf("control request timeout required")
	}
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return CallResult{}, fmt.Errorf("connect to Zen control socket: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return CallResult{RequestMayHaveArrived: true}, fmt.Errorf(
			"send control request: %w",
			err,
		)
	}
	result := CallResult{RequestMayHaveArrived: true}
	var resp Response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return result, fmt.Errorf("read control response: %w", err)
	}
	result.Response = resp
	return result, nil
}
