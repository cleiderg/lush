package protocol

import (
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
)

func TestRoundTripHallClientMessages(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	HallClaimStart(b)
	HallClaimAddHallId(b, 6)
	var claim HallClaim
	tbl := open(t, finish(b, MessageHallClaim, HallClaimEnd(b)), MessageHallClaim)
	claim.Init(tbl.Bytes, tbl.Pos)
	if claim.HallId() != 6 {
		t.Fatal("HallClaim mismatch")
	}

	b = flatbuffers.NewBuilder(16)
	HallGiveUpStart(b)
	open(t, finish(b, MessageHallGiveUp, HallGiveUpEnd(b)), MessageHallGiveUp)

	for _, door := range []HallDoorId{HallDoorIdOuter0, HallDoorIdOuter1, HallDoorIdInner} {
		for _, verb := range []DoorVerb{DoorVerbOpen, DoorVerbClose, DoorVerbLock, DoorVerbUnlock, DoorVerbForce, DoorVerbRepair} {
			b = flatbuffers.NewBuilder(32)
			DoorActionStart(b)
			DoorActionAddHallId(b, 3)
			DoorActionAddDoor(b, door)
			DoorActionAddAction(b, verb)
			var a DoorAction
			tbl = open(t, finish(b, MessageDoorAction, DoorActionEnd(b)), MessageDoorAction)
			a.Init(tbl.Bytes, tbl.Pos)
			if a.HallId() != 3 || a.Door() != door || a.Action() != verb {
				t.Fatalf("DoorAction mismatch for %v %v", door, verb)
			}
		}
	}

	b = flatbuffers.NewBuilder(16)
	DoorLogRequestStart(b)
	open(t, finish(b, MessageDoorLogRequest, DoorLogRequestEnd(b)), MessageDoorLogRequest)

	b = flatbuffers.NewBuilder(32)
	VaultDepositStart(b)
	VaultDepositAddAmount(b, 5_000_000_000)
	var d VaultDeposit
	tbl = open(t, finish(b, MessageVaultDeposit, VaultDepositEnd(b)), MessageVaultDeposit)
	d.Init(tbl.Bytes, tbl.Pos)
	if d.Amount() != 5_000_000_000 {
		t.Fatal("VaultDeposit mismatch")
	}

	b = flatbuffers.NewBuilder(32)
	VaultWithdrawStart(b)
	VaultWithdrawAddAmount(b, 750)
	var w VaultWithdraw
	tbl = open(t, finish(b, MessageVaultWithdraw, VaultWithdrawEnd(b)), MessageVaultWithdraw)
	w.Init(tbl.Bytes, tbl.Pos)
	if w.Amount() != 750 {
		t.Fatal("VaultWithdraw mismatch")
	}
}

func TestRoundTripHallsState(t *testing.T) {
	b := flatbuffers.NewBuilder(128)
	name := b.CreateString("Anteaters")
	HallInfoStart(b)
	HallInfoAddId(b, 1)
	HallInfoAddGroupId(b, 70000)
	HallInfoAddGroupName(b, name)
	HallInfoAddBanner(b, 11)
	owned := HallInfoEnd(b)
	HallInfoStart(b)
	HallInfoAddId(b, 2)
	empty := HallInfoEnd(b)
	HallsStateStartHallsVector(b, 2)
	b.PrependUOffsetT(empty)
	b.PrependUOffsetT(owned)
	vec := b.EndVector(2)
	HallsStateStart(b)
	HallsStateAddHalls(b, vec)
	var s HallsState
	tbl := open(t, finish(b, MessageHallsState, HallsStateEnd(b)), MessageHallsState)
	s.Init(tbl.Bytes, tbl.Pos)
	if s.HallsLength() != 2 {
		t.Fatalf("halls = %d, want 2", s.HallsLength())
	}
	var h HallInfo
	s.Halls(&h, 0)
	if h.Id() != 1 || h.GroupId() != 70000 || string(h.GroupName()) != "Anteaters" || h.Banner() != 11 {
		t.Fatal("owned hall mismatch")
	}
	s.Halls(&h, 1)
	if h.Id() != 2 || h.GroupId() != 0 || h.GroupName() != nil {
		t.Fatal("empty hall mismatch")
	}
}

func TestRoundTripDoorState(t *testing.T) {
	for _, st := range []DoorPublicState{DoorPublicStateOpen, DoorPublicStateShut, DoorPublicStateBroken} {
		b := flatbuffers.NewBuilder(32)
		DoorStateStart(b)
		DoorStateAddHallId(b, 4)
		DoorStateAddDoor(b, HallDoorIdInner)
		DoorStateAddState(b, st)
		DoorStateAddBeingForced(b, true)
		DoorStateAddHp(b, 275)
		var d DoorState
		tbl := open(t, finish(b, MessageDoorState, DoorStateEnd(b)), MessageDoorState)
		d.Init(tbl.Bytes, tbl.Pos)
		if d.HallId() != 4 || d.Door() != HallDoorIdInner || d.State() != st || !d.BeingForced() || d.Hp() != 275 {
			t.Fatalf("DoorState mismatch for %v", st)
		}
	}
}

// A closed door must not be distinguishable from a locked one (groups.md:
// Doors): the public state has no locked value and DoorState has no lock field.
func TestDoorStateHidesLock(t *testing.T) {
	for v, name := range EnumNamesDoorPublicState {
		switch name {
		case "Open", "Shut", "Broken":
		default:
			t.Fatalf("DoorPublicState %d = %q leaks lock state", v, name)
		}
	}
	if len(EnumNamesDoorPublicState) != 3 {
		t.Fatalf("DoorPublicState has %d values, want 3", len(EnumNamesDoorPublicState))
	}
	var methods []string
	typ := reflect.TypeOf(&DoorState{})
	for i := 0; i < typ.NumMethod(); i++ {
		methods = append(methods, typ.Method(i).Name)
	}
	for _, m := range methods {
		switch m {
		case "Init", "Table", "HallId", "MutateHallId", "Door", "MutateDoor", "State", "MutateState",
			"BeingForced", "MutateBeingForced", "Hp", "MutateHp":
		default:
			t.Fatalf("DoorState has unexpected accessor %s", m)
		}
	}
}

func TestRoundTripDoorLog(t *testing.T) {
	b := flatbuffers.NewBuilder(256)
	actions := []DoorLogAction{DoorLogActionOpen, DoorLogActionClose, DoorLogActionLock, DoorLogActionUnlock, DoorLogActionBreak, DoorLogActionRepair}
	var offs []flatbuffers.UOffsetT
	for i, a := range actions {
		n := b.CreateString("Peter")
		DoorLogEntryStart(b)
		DoorLogEntryAddDoor(b, HallDoorId(i%3))
		DoorLogEntryAddAction(b, a)
		DoorLogEntryAddPlayerName(b, n)
		DoorLogEntryAddTimeMs(b, 1_800_000_000_000+uint64(i))
		offs = append(offs, DoorLogEntryEnd(b))
	}
	DoorLogStartEntriesVector(b, len(offs))
	for i := len(offs) - 1; i >= 0; i-- {
		b.PrependUOffsetT(offs[i])
	}
	vec := b.EndVector(len(offs))
	DoorLogStart(b)
	DoorLogAddHallId(b, 5)
	DoorLogAddEntries(b, vec)
	var l DoorLog
	tbl := open(t, finish(b, MessageDoorLog, DoorLogEnd(b)), MessageDoorLog)
	l.Init(tbl.Bytes, tbl.Pos)
	if l.HallId() != 5 || l.EntriesLength() != len(actions) {
		t.Fatal("DoorLog header mismatch")
	}
	for i, a := range actions {
		var e DoorLogEntry
		l.Entries(&e, i)
		if e.Door() != HallDoorId(i%3) || e.Action() != a || string(e.PlayerName()) != "Peter" || e.TimeMs() != 1_800_000_000_000+uint64(i) {
			t.Fatalf("DoorLog entry %d mismatch", i)
		}
	}
}

func TestRoundTripHallAlarmAndResult(t *testing.T) {
	for _, c := range []AlarmCause{AlarmCauseDoor, AlarmCauseVault} {
		b := flatbuffers.NewBuilder(32)
		HallAlarmStart(b)
		HallAlarmAddHallId(b, 2)
		HallAlarmAddCause(b, c)
		var a HallAlarm
		tbl := open(t, finish(b, MessageHallAlarm, HallAlarmEnd(b)), MessageHallAlarm)
		a.Init(tbl.Bytes, tbl.Pos)
		if a.HallId() != 2 || a.Cause() != c {
			t.Fatalf("HallAlarm mismatch for %v", c)
		}
	}
	for code := range EnumNamesHallResultCode {
		b := flatbuffers.NewBuilder(32)
		HallResultStart(b)
		HallResultAddCode(b, code)
		var r HallResult
		tbl := open(t, finish(b, MessageHallResult, HallResultEnd(b)), MessageHallResult)
		r.Init(tbl.Bytes, tbl.Pos)
		if r.Code() != code {
			t.Fatalf("HallResult mismatch for %v", code)
		}
	}
}
