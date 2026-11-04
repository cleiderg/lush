package protocol

import (
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
)

func TestRoundTripGroupClientMessages(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	n := b.CreateString("Anteaters")
	GroupFoundStart(b)
	GroupFoundAddName(b, n)
	GroupFoundAddBanner(b, 11)
	var f GroupFound
	tbl := open(t, finish(b, MessageGroupFound, GroupFoundEnd(b)), MessageGroupFound)
	f.Init(tbl.Bytes, tbl.Pos)
	if string(f.Name()) != "Anteaters" || f.Banner() != 11 {
		t.Fatal("GroupFound mismatch")
	}

	b = flatbuffers.NewBuilder(32)
	GroupInviteStart(b)
	GroupInviteAddTargetId(b, 321)
	var inv GroupInvite
	tbl = open(t, finish(b, MessageGroupInvite, GroupInviteEnd(b)), MessageGroupInvite)
	inv.Init(tbl.Bytes, tbl.Pos)
	if inv.TargetId() != 321 {
		t.Fatal("GroupInvite mismatch")
	}

	b = flatbuffers.NewBuilder(32)
	GroupInviteReplyStart(b)
	GroupInviteReplyAddGroupId(b, 70000)
	GroupInviteReplyAddAccept(b, true)
	var ir GroupInviteReply
	tbl = open(t, finish(b, MessageGroupInviteReply, GroupInviteReplyEnd(b)), MessageGroupInviteReply)
	ir.Init(tbl.Bytes, tbl.Pos)
	if ir.GroupId() != 70000 || !ir.Accept() {
		t.Fatal("GroupInviteReply mismatch")
	}

	b = flatbuffers.NewBuilder(32)
	GroupSetRankStart(b)
	GroupSetRankAddMember(b, 99)
	GroupSetRankAddPromote(b, true)
	var sr GroupSetRank
	tbl = open(t, finish(b, MessageGroupSetRank, GroupSetRankEnd(b)), MessageGroupSetRank)
	sr.Init(tbl.Bytes, tbl.Pos)
	if sr.Member() != 99 || !sr.Promote() {
		t.Fatal("GroupSetRank mismatch")
	}

	b = flatbuffers.NewBuilder(32)
	GroupKickStart(b)
	GroupKickAddMember(b, 5)
	var k GroupKick
	tbl = open(t, finish(b, MessageGroupKick, GroupKickEnd(b)), MessageGroupKick)
	k.Init(tbl.Bytes, tbl.Pos)
	if k.Member() != 5 {
		t.Fatal("GroupKick mismatch")
	}

	b = flatbuffers.NewBuilder(32)
	GroupHandoverStart(b)
	GroupHandoverAddMember(b, 6)
	var h GroupHandover
	tbl = open(t, finish(b, MessageGroupHandover, GroupHandoverEnd(b)), MessageGroupHandover)
	h.Init(tbl.Bytes, tbl.Pos)
	if h.Member() != 6 {
		t.Fatal("GroupHandover mismatch")
	}

	b = flatbuffers.NewBuilder(32)
	GroupSetPermissionStart(b)
	GroupSetPermissionAddRank(b, GroupRankOfficer)
	GroupSetPermissionAddPermission(b, GroupPermissionWithdraw)
	GroupSetPermissionAddOn(b, true)
	var sp GroupSetPermission
	tbl = open(t, finish(b, MessageGroupSetPermission, GroupSetPermissionEnd(b)), MessageGroupSetPermission)
	sp.Init(tbl.Bytes, tbl.Pos)
	if sp.Rank() != GroupRankOfficer || sp.Permission() != GroupPermissionWithdraw || !sp.On() {
		t.Fatal("GroupSetPermission mismatch")
	}

	b = flatbuffers.NewBuilder(16)
	GroupLeaveStart(b)
	open(t, finish(b, MessageGroupLeave, GroupLeaveEnd(b)), MessageGroupLeave)
	b = flatbuffers.NewBuilder(16)
	GroupDisbandStart(b)
	open(t, finish(b, MessageGroupDisband, GroupDisbandEnd(b)), MessageGroupDisband)
}

func TestRoundTripGroupServerMessages(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	in := b.CreateString("Ann")
	gn := b.CreateString("Anteaters")
	GroupInvitedStart(b)
	GroupInvitedAddInviterName(b, in)
	GroupInvitedAddGroupName(b, gn)
	GroupInvitedAddGroupId(b, 12)
	var inv GroupInvited
	tbl := open(t, finish(b, MessageGroupInvited, GroupInvitedEnd(b)), MessageGroupInvited)
	inv.Init(tbl.Bytes, tbl.Pos)
	if string(inv.InviterName()) != "Ann" || string(inv.GroupName()) != "Anteaters" || inv.GroupId() != 12 {
		t.Fatal("GroupInvited mismatch")
	}

	b = flatbuffers.NewBuilder(256)
	n1, n2 := b.CreateString("Ann"), b.CreateString("Bob")
	GroupMemberStart(b)
	GroupMemberAddKey(b, 1)
	GroupMemberAddName(b, n1)
	GroupMemberAddRank(b, GroupRankLeader)
	GroupMemberAddOnline(b, true)
	m1 := GroupMemberEnd(b)
	GroupMemberStart(b)
	GroupMemberAddKey(b, 2)
	GroupMemberAddName(b, n2)
	m2 := GroupMemberEnd(b)
	GroupStateStartMembersVector(b, 2)
	b.PrependUOffsetT(m2)
	b.PrependUOffsetT(m1)
	members := b.EndVector(2)
	GroupStateStartPermissionsVector(b, 2)
	CreateGroupPermissionSetting(b, GroupRankOfficer, GroupPermissionKeys, true)
	CreateGroupPermissionSetting(b, GroupRankMember, GroupPermissionVaultRoom, false)
	perms := b.EndVector(2)
	name := b.CreateString("Anteaters")
	GroupStateStart(b)
	GroupStateAddGroupId(b, 12)
	GroupStateAddName(b, name)
	GroupStateAddBanner(b, 3)
	GroupStateAddOwnRank(b, GroupRankLeader)
	GroupStateAddPermissions(b, perms)
	GroupStateAddVaultGold(b, 1<<40)
	GroupStateAddHallId(b, 4)
	GroupStateAddMembers(b, members)
	var s GroupState
	tbl = open(t, finish(b, MessageGroupState, GroupStateEnd(b)), MessageGroupState)
	s.Init(tbl.Bytes, tbl.Pos)
	var m GroupMember
	var ps GroupPermissionSetting
	if s.GroupId() != 12 || string(s.Name()) != "Anteaters" || s.Banner() != 3 || s.OwnRank() != GroupRankLeader ||
		s.VaultGold() != 1<<40 || s.HallId() != 4 || s.MembersLength() != 2 || s.PermissionsLength() != 2 {
		t.Fatal("GroupState mismatch")
	}
	if !s.Members(&m, 0) || m.Key() != 1 || string(m.Name()) != "Ann" || m.Rank() != GroupRankLeader || !m.Online() {
		t.Fatal("member 0 mismatch")
	}
	if !s.Members(&m, 1) || m.Key() != 2 || string(m.Name()) != "Bob" || m.Rank() != GroupRankRecruit || m.Online() {
		t.Fatal("member 1 mismatch")
	}
	if !s.Permissions(&ps, 1) || ps.Rank() != GroupRankOfficer || ps.Permission() != GroupPermissionKeys || !ps.On() {
		t.Fatal("permission mismatch")
	}

	for code := GroupResultCodeOk; code <= GroupResultCodeNotLeader; code++ {
		b = flatbuffers.NewBuilder(16)
		GroupResultStart(b)
		GroupResultAddCode(b, code)
		var r GroupResult
		tbl = open(t, finish(b, MessageGroupResult, GroupResultEnd(b)), MessageGroupResult)
		r.Init(tbl.Bytes, tbl.Pos)
		if r.Code() != code {
			t.Fatalf("code = %v, want %v", r.Code(), code)
		}
	}
}

func TestChatChannelDefaultsLocal(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	txt := b.CreateString("hi")
	ChatSendStart(b)
	ChatSendAddText(b, txt)
	var s ChatSend
	tbl := open(t, finish(b, MessageChatSend, ChatSendEnd(b)), MessageChatSend)
	s.Init(tbl.Bytes, tbl.Pos)
	if s.Channel() != ChatChannelLocal {
		t.Fatal("old ChatSend should be local")
	}

	b = flatbuffers.NewBuilder(64)
	txt = b.CreateString("hi")
	ChatMessageStart(b)
	ChatMessageAddSpeakerId(b, 7)
	ChatMessageAddText(b, txt)
	var c ChatMessage
	tbl = open(t, finish(b, MessageChatMessage, ChatMessageEnd(b)), MessageChatMessage)
	c.Init(tbl.Bytes, tbl.Pos)
	if c.Channel() != ChatChannelLocal || len(c.SpeakerName()) != 0 {
		t.Fatal("old ChatMessage should be local with no name")
	}

	b = flatbuffers.NewBuilder(64)
	txt = b.CreateString("hello team")
	nm := b.CreateString("Ann")
	ChatSendStart(b)
	ChatSendAddText(b, txt)
	ChatSendAddChannel(b, ChatChannelGroup)
	tbl = open(t, finish(b, MessageChatSend, ChatSendEnd(b)), MessageChatSend)
	s.Init(tbl.Bytes, tbl.Pos)
	if s.Channel() != ChatChannelGroup {
		t.Fatal("group channel lost")
	}
	_ = nm
	b = flatbuffers.NewBuilder(64)
	txt = b.CreateString("hello team")
	nm = b.CreateString("Ann")
	ChatMessageStart(b)
	ChatMessageAddText(b, txt)
	ChatMessageAddChannel(b, ChatChannelGroup)
	ChatMessageAddSpeakerName(b, nm)
	tbl = open(t, finish(b, MessageChatMessage, ChatMessageEnd(b)), MessageChatMessage)
	c.Init(tbl.Bytes, tbl.Pos)
	if c.Channel() != ChatChannelGroup || string(c.SpeakerName()) != "Ann" {
		t.Fatal("ChatMessage group fields lost")
	}
}

func TestEntityInfoGroup(t *testing.T) {
	b := flatbuffers.NewBuilder(64)
	gn := b.CreateString("Anteaters")
	EntityInfoStart(b)
	EntityInfoAddId(b, 5)
	EntityInfoAddGroupId(b, 12)
	EntityInfoAddGroupBanner(b, 9)
	EntityInfoAddGroupName(b, gn)
	var e EntityInfo
	tbl := open(t, finish(b, MessageEntityInfo, EntityInfoEnd(b)), MessageEntityInfo)
	e.Init(tbl.Bytes, tbl.Pos)
	if e.GroupId() != 12 || e.GroupBanner() != 9 || string(e.GroupName()) != "Anteaters" {
		t.Fatal("EntityInfo group mismatch")
	}

	b = flatbuffers.NewBuilder(64)
	EntityInfoStart(b)
	EntityInfoAddId(b, 5)
	tbl = open(t, finish(b, MessageEntityInfo, EntityInfoEnd(b)), MessageEntityInfo)
	e.Init(tbl.Bytes, tbl.Pos)
	if e.GroupId() != 0 || len(e.GroupName()) != 0 {
		t.Fatal("no group should be zero")
	}
}
