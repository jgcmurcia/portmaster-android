package app_interface

import (
	"errors"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

type commandReceiver struct {
	commands []string
	err      error
}

func (r *commandReceiver) CallFunction(name string, args []byte) ([]byte, error) {
	if name != "SendServiceCommand" {
		return nil, errors.New("unexpected function: " + name)
	}
	var command string
	if err := cbor.Unmarshal(args, &command); err != nil {
		return nil, err
	}
	r.commands = append(r.commands, command)
	return nil, r.err
}

func TestServiceShutdownWorksWithoutActivity(t *testing.T) {
	RemoveActivityFunctionReference()
	service := &commandReceiver{}
	SetServiceFunctions(service)
	t.Cleanup(RemoveServiceFunctionReference)
	if err := SendServicesCommand("shutdown"); err != nil {
		t.Fatal(err)
	}
	if len(service.commands) != 1 || service.commands[0] != "shutdown" {
		t.Fatalf("service commands: %v", service.commands)
	}
}

func TestServiceCommandDoesNotFallbackToActivityAfterServiceFailure(t *testing.T) {
	service := &commandReceiver{err: errors.New("stop failed")}
	activity := &commandReceiver{}
	SetServiceFunctions(service)
	SetActivityFunctions(activity)
	t.Cleanup(RemoveServiceFunctionReference)
	t.Cleanup(RemoveActivityFunctionReference)
	if err := SendServicesCommand("shutdown"); err == nil {
		t.Fatal("expected service failure")
	}
	if len(activity.commands) != 0 {
		t.Fatal("shutdown must not restart the activity permission flow")
	}
}

func TestEnableUsesActivityWhenServiceIsAbsent(t *testing.T) {
	RemoveServiceFunctionReference()
	activity := &commandReceiver{}
	SetActivityFunctions(activity)
	t.Cleanup(RemoveActivityFunctionReference)
	if err := SendServicesCommand("keep_alive"); err != nil {
		t.Fatal(err)
	}
	if len(activity.commands) != 1 || activity.commands[0] != "keep_alive" {
		t.Fatalf("activity commands: %v", activity.commands)
	}
}

func TestActivityCallsFailCleanlyWhenActivityIsAbsent(t *testing.T) {
	RemoveActivityFunctionReference()
	if err := SendUIEvent(Event{Name: "test"}); err == nil {
		t.Fatal("expected SendUIEvent to report a missing Activity")
	}
	if err := MinimizeApp(); err == nil {
		t.Fatal("expected MinimizeApp to report a missing Activity")
	}
}
