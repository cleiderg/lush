package session

import (
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"github.com/cleiderg/lush/server/internal/protocol"
	"github.com/cleiderg/lush/server/internal/store"
	"github.com/cleiderg/lush/server/internal/systems/bank"
	"github.com/cleiderg/lush/server/internal/systems/gamedata"
	"github.com/cleiderg/lush/server/internal/world"
)

func bankItemFrame(typ protocol.Message, item uint16, amount uint32) []byte {
	return frame(typ, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		if typ == protocol.MessageBankDeposit {
			protocol.BankDepositStart(b)
			protocol.BankDepositAddItemId(b, item)
			protocol.BankDepositAddAmount(b, amount)
			return protocol.BankDepositEnd(b)
		}
		protocol.BankWithdrawStart(b)
		protocol.BankWithdrawAddItemId(b, item)
		protocol.BankWithdrawAddAmount(b, amount)
		return protocol.BankWithdrawEnd(b)
	})
}

func emptyFrame(typ protocol.Message, start func(*flatbuffers.Builder), end func(*flatbuffers.Builder) flatbuffers.UOffsetT) []byte {
	return frame(typ, func(b *flatbuffers.Builder) flatbuffers.UOffsetT {
		start(b)
		return end(b)
	})
}

// AC1 (#471): each bank message becomes the matching command; the item
// messages carry the item id and BankAmount.All becomes bank.All.
func TestDecodeBankCommands(t *testing.T) {
	const id = world.PlayerID(7)
	tests := []struct {
		name string
		buf  []byte
		want world.Command
	}{
		{"open", emptyFrame(protocol.MessageBankOpen, protocol.BankOpenStart, protocol.BankOpenEnd), bank.OpenCommand{Player: id}},
		{"close", emptyFrame(protocol.MessageBankClose, protocol.BankCloseStart, protocol.BankCloseEnd), bank.CloseCommand{Player: id}},
		{"deposit", bankItemFrame(protocol.MessageBankDeposit, 9, 5), DepositItemCommand{Player: id, Item: 9, Amount: 5}},
		{"deposit all", bankItemFrame(protocol.MessageBankDeposit, 9, uint32(protocol.BankAmountAll)), DepositItemCommand{Player: id, Item: 9, Amount: bank.All}},
		{"withdraw", bankItemFrame(protocol.MessageBankWithdraw, 4, 1), WithdrawItemCommand{Player: id, Item: 4, Amount: 1}},
		{"withdraw all", bankItemFrame(protocol.MessageBankWithdraw, 4, uint32(protocol.BankAmountAll)), WithdrawItemCommand{Player: id, Item: 4, Amount: bank.All}},
		{"deposit inventory", emptyFrame(protocol.MessageBankDepositInventory, protocol.BankDepositInventoryStart, protocol.BankDepositInventoryEnd), bank.DepositInventoryCommand{Player: id}},
		{"deposit equipment", emptyFrame(protocol.MessageBankDepositEquipment, protocol.BankDepositEquipmentStart, protocol.BankDepositEquipmentEnd), bank.DepositEquipmentCommand{Player: id}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := decode(tt.buf)
			if err != nil {
				t.Fatal(err)
			}
			if got := m.command(id); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("command %#v, want %#v", got, tt.want)
			}
		})
	}
}

// bankMoves turns a bank system result into the store moves that make the
// same change to the stored holdings.
func TestBankMoves(t *testing.T) {
	d, err := gamedata.Default()
	if err != nil {
		t.Fatal(err)
	}
	sword := d.Rules.StartingKit.Inventory[0].Item
	before := world.NewInventory(d)
	before.Slots[0] = world.Stack{Item: sword, Qty: 3}
	before.Slots[2] = world.Stack{Item: sword, Qty: 1}
	before.Equipment.Weapon = 5
	after := cloneInventory(before)
	after.Slots[0] = world.Stack{Item: sword, Qty: 1} // two deposited
	after.Slots[2] = world.Stack{}                    // one deposited
	after.Slots[5] = world.Stack{Item: 8, Qty: 4}     // four withdrawn
	after.Equipment.Weapon = 0                        // worn item deposited
	got, err := bankMoves(&before, &after)
	if err != nil {
		t.Fatal(err)
	}
	want := []store.BankMove{
		{Deposit: true, Slot: 0, Item: int32(sword), Qty: 2},
		{Deposit: true, Slot: 2, Item: int32(sword), Qty: 1},
		{Slot: 5, Item: 8, Qty: 4},
		{Deposit: true, Equipment: true, Slot: int(store.EquipWeapon), Item: 5, Qty: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("moves %+v, want %+v", got, want)
	}
	if _, err := bankMoves(&before, &before); err == nil {
		t.Error("a transfer that moves nothing is accepted")
	}
	gold := cloneInventory(before)
	gold.Gold++
	if _, err := bankMoves(&before, &gold); err == nil {
		t.Error("a transfer that changes gold is accepted")
	}
}
