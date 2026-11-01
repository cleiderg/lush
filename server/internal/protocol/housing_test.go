package protocol

import (
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
)

func TestRoundTripHousingState(t *testing.T) {
	b := flatbuffers.NewBuilder(256)
	owner := b.CreateString("Peter")
	HouseInfoStart(b)
	HouseInfoAddId(b, 3)
	HouseInfoAddAreaId(b, 1)
	HouseInfoAddOwnerName(b, owner)
	HouseInfoAddMine(b, true)
	HouseInfoAddPaidUntil(b, 1795000000)
	HouseInfoAddState(b, HouseStateWarning)
	HouseInfoAddOpen(b, true)
	h1 := HouseInfoEnd(b)
	HouseInfoStart(b)
	HouseInfoAddId(b, 4)
	h2 := HouseInfoEnd(b)
	HousingStateStartHousesVector(b, 2)
	b.PrependUOffsetT(h2)
	b.PrependUOffsetT(h1)
	hv := b.EndVector(2)
	HousingStateStart(b)
	HousingStateAddHouses(b, hv)
	buf := finish(b, MessageHousingState, HousingStateEnd(b))

	var m HousingState
	tbl := open(t, buf, MessageHousingState)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.HousesLength() != 2 {
		t.Fatalf("houses = %d", m.HousesLength())
	}
	var h HouseInfo
	m.Houses(&h, 0)
	if h.Id() != 3 || h.AreaId() != 1 || string(h.OwnerName()) != "Peter" || !h.Mine() ||
		h.PaidUntil() != 1795000000 || h.State() != HouseStateWarning || !h.Open() {
		t.Fatalf("house 0 mismatch")
	}
	m.Houses(&h, 1)
	if h.Id() != 4 || len(h.OwnerName()) != 0 || h.Mine() || h.PaidUntil() != 0 ||
		h.State() != HouseStateEmpty || h.Open() {
		t.Fatalf("house 1 mismatch")
	}
}

func TestRoundTripHouseClaimRentPay(t *testing.T) {
	b := flatbuffers.NewBuilder(32)
	HouseClaimStart(b)
	HouseClaimAddHouseId(b, 39)
	buf := finish(b, MessageHouseClaim, HouseClaimEnd(b))
	var c HouseClaim
	tbl := open(t, buf, MessageHouseClaim)
	c.Init(tbl.Bytes, tbl.Pos)
	if c.HouseId() != 39 {
		t.Fatalf("house id = %d", c.HouseId())
	}

	b = flatbuffers.NewBuilder(32)
	RentPayStart(b)
	RentPayAddHouseId(b, 7)
	RentPayAddMonths(b, 6)
	buf = finish(b, MessageRentPay, RentPayEnd(b))
	var r RentPay
	tbl = open(t, buf, MessageRentPay)
	r.Init(tbl.Bytes, tbl.Pos)
	if r.HouseId() != 7 || r.Months() != 6 {
		t.Fatalf("rent pay = %d %d", r.HouseId(), r.Months())
	}
}

func TestRoundTripHousingResult(t *testing.T) {
	codes := []HousingResultCode{
		HousingResultCodeOk, HousingResultCodeAlreadyOwned, HousingResultCodeOwnsAnother,
		HousingResultCodeNotEnoughGold, HousingResultCodeTooFar, HousingResultCodeTooManyMonths,
		HousingResultCodeNotOwner, HousingResultCodeSaveFailed,
	}
	for want, code := range codes {
		if int(code) != want {
			t.Fatalf("code %d has value %d", want, code)
		}
		b := flatbuffers.NewBuilder(32)
		HousingResultStart(b)
		HousingResultAddCode(b, code)
		buf := finish(b, MessageHousingResult, HousingResultEnd(b))
		var m HousingResult
		tbl := open(t, buf, MessageHousingResult)
		m.Init(tbl.Bytes, tbl.Pos)
		if m.Code() != code {
			t.Fatalf("code = %v, want %v", m.Code(), code)
		}
	}
}

func TestRoundTripHomeState(t *testing.T) {
	b := flatbuffers.NewBuilder(256)
	owner := b.CreateString("Zot")
	PlacedItemStart(b)
	PlacedItemAddPlacedId(b, 100000)
	PlacedItemAddItemId(b, 205)
	PlacedItemAddX(b, 3)
	PlacedItemAddY(b, 7)
	PlacedItemAddRotation(b, 2)
	PlacedItemAddLayer(b, 1)
	PlacedItemAddParentPlacedId(b, 99999)
	p := PlacedItemEnd(b)
	HomeStateStartItemsVector(b, 1)
	b.PrependUOffsetT(p)
	iv := b.EndVector(1)
	HomeStateStart(b)
	HomeStateAddHouseId(b, 12)
	HomeStateAddOwnerName(b, owner)
	HomeStateAddWallpaperItemId(b, 300)
	HomeStateAddFloorItemId(b, 301)
	HomeStateAddItems(b, iv)
	buf := finish(b, MessageHomeState, HomeStateEnd(b))

	var m HomeState
	tbl := open(t, buf, MessageHomeState)
	m.Init(tbl.Bytes, tbl.Pos)
	if m.HouseId() != 12 || string(m.OwnerName()) != "Zot" || m.WallpaperItemId() != 300 ||
		m.FloorItemId() != 301 || m.ItemsLength() != 1 {
		t.Fatalf("home state mismatch")
	}
	var got PlacedItem
	m.Items(&got, 0)
	if got.PlacedId() != 100000 || got.ItemId() != 205 || got.X() != 3 || got.Y() != 7 ||
		got.Rotation() != 2 || got.Layer() != 1 || got.ParentPlacedId() != 99999 {
		t.Fatalf("placed item mismatch")
	}
}

func TestItemDefFurnitureFields(t *testing.T) {
	b := flatbuffers.NewBuilder(128)
	ItemDefStart(b)
	ItemDefAddId(b, 205)
	ItemDefAddFootprintW(b, 2)
	ItemDefAddFootprintD(b, 1)
	ItemDefAddPlacement(b, PlacementKindFloorOrSurface)
	ItemDefAddSurfaceSlots(b, 4)
	d1 := ItemDefEnd(b)
	ItemDefStart(b)
	ItemDefAddId(b, 1)
	d2 := ItemDefEnd(b)
	ItemCatalogStartItemsVector(b, 2)
	b.PrependUOffsetT(d2)
	b.PrependUOffsetT(d1)
	iv := b.EndVector(2)
	ItemCatalogStart(b)
	ItemCatalogAddItems(b, iv)
	buf := finish(b, MessageItemCatalog, ItemCatalogEnd(b))

	var m ItemCatalog
	tbl := open(t, buf, MessageItemCatalog)
	m.Init(tbl.Bytes, tbl.Pos)
	var d ItemDef
	m.Items(&d, 0)
	if d.FootprintW() != 2 || d.FootprintD() != 1 || d.Placement() != PlacementKindFloorOrSurface || d.SurfaceSlots() != 4 {
		t.Fatal("furniture fields mismatch")
	}
	m.Items(&d, 1)
	if d.FootprintW() != 0 || d.FootprintD() != 0 || d.Placement() != PlacementKindNone || d.SurfaceSlots() != 0 {
		t.Fatal("defaults should mean not furniture")
	}
}
