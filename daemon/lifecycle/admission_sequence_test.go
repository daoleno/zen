package lifecycle

import (
	"testing"
	"time"
)

func TestValidatePreparedAdmissionSerialization(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second AdmissionStatus
		valid         bool
	}{
		{"ambiguous and prepared", AdmissionAmbiguous, AdmissionPrepared, true},
		{"both ambiguous", AdmissionAmbiguous, AdmissionAmbiguous, true},
		{"both prepared", AdmissionPrepared, AdmissionPrepared, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &State{ID: "work", Admissions: map[TurnToken]*AdmissionState{}}
			for token, status := range map[TurnToken]AdmissionStatus{"first": tc.first, "second": tc.second} {
				state.Admissions[token] = &AdmissionState{
					TurnToken: token, SessionID: "worker", AttemptedAt: time.Unix(1, 0), Status: status,
				}
			}
			err := validateLifecycleDatabase(lifecycleDatabase{
				Schema: lifecycleStoreSchema, NextSeq: 1,
				Works: map[WorkID]*State{state.ID: state}, Events: []Event{},
			})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}
}

func TestValidateAdmissionAcceptanceSequence(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sequence uint64
		valid    bool
	}{
		{"valid", 2, true},
		{"missing", 0, false},
		{"duplicate", 1, false},
		{"future", 3, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := &State{ID: "work", Admissions: map[TurnToken]*AdmissionState{}}
			for token, seq := range map[TurnToken]uint64{"first": 1, "second": tc.sequence} {
				state.Admissions[token] = &AdmissionState{
					TurnToken: token, SessionID: "worker", AttemptedAt: time.Unix(1, 0),
					Status: AdmissionAccepted, AcceptedSeq: seq,
				}
			}
			err := validateLifecycleDatabase(lifecycleDatabase{
				Schema: lifecycleStoreSchema, NextSeq: 3,
				Works: map[WorkID]*State{state.ID: state}, Events: []Event{},
			})
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
		})
	}
}
