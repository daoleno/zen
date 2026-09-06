package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/daoleno/zen/daemon/control"
	"github.com/daoleno/zen/daemon/lifecycle"
)

func runWorkerReceipt(args []string, stderr io.Writer) error {
	fs := flag.NewFlagSet("zen worker receipt", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cfg := cliConfig{json: true}
	req := control.Request{Type: "worker_receipt"}
	fs.StringVar(&cfg.stateDir, "state-dir", "", "state directory for daemon control socket")
	fs.BoolVar(&cfg.json, "json", true, "print JSON output")
	fs.StringVar(&req.WorkID, "work-id", "", "exact Work id")
	fs.StringVar(&req.WorkerID, "id", "", "exact Worker Session id")
	fs.StringVar(&req.TurnID, "turn-id", "", "exact proposed input Turn token")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || strings.TrimSpace(req.WorkID) == "" || strings.TrimSpace(req.WorkerID) == "" || strings.TrimSpace(req.TurnID) == "" {
		return fmt.Errorf("receipt requires -work-id, -id and -turn-id")
	}
	resp, err := callControl(cfg, req)
	if err != nil {
		return err
	}
	return writeControlResponse(os.Stdout, resp, cfg.json)
}

func (a *controlApp) handleWorkerReceipt(req control.Request) control.Response {
	if a.brainStore == nil {
		return control.ErrorResponse("brain_unavailable", "Brain lifecycle is unavailable.")
	}
	if strings.TrimSpace(req.WorkID) == "" || strings.TrimSpace(req.WorkerID) == "" || strings.TrimSpace(req.TurnID) == "" {
		return control.ErrorResponse("missing_receipt_identity", "Work, Worker Session and Turn token are required.")
	}
	state, err := a.brainStore.FSM().State(lifecycle.WorkID(req.WorkID))
	if err != nil {
		return control.ErrorResponse("receipt_not_found", err.Error())
	}
	admission := state.AdmissionByToken(lifecycle.TurnToken(req.TurnID))
	if admission == nil || admission.SessionID != req.WorkerID || admission.ClaimToken != "" {
		return control.ErrorResponse("receipt_not_found", "No exact Worker admission exists.")
	}
	return control.Response{OK: true, WorkerReceipt: &control.WorkerReceipt{
		WorkID: state.ID, Admission: *admission,
		OwnsAttempt: state.Attempt != nil && state.Attempt.SessionID == req.WorkerID && state.Attempt.TurnToken == admission.TurnToken,
	}}
}
