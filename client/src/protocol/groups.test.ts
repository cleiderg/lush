import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  ChatChannel, ChatMessage, ChatSend, EntityInfo, Envelope, GroupDisband, GroupFound, GroupHandover, GroupInvite,
  GroupInviteReply, GroupInvited, GroupKick, GroupLeave, GroupMember, GroupPermission, GroupPermissionSetting,
  GroupRank, GroupResult, GroupResultCode, GroupSetPermission, GroupSetRank, GroupState, Message,
} from './index.js';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array();
}

function open<T>(buf: Uint8Array, type: Message, m: T): T {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(buf));
  expect(env.msgType()).toBe(type);
  return env.msg(m as never) as T;
}

describe('group protocol', () => {
  it('round-trips client messages', () => {
    let b = new flatbuffers.Builder(64);
    const n = b.createString('Anteaters');
    const f = open(finish(b, Message.GroupFound, GroupFound.createGroupFound(b, n, 11)), Message.GroupFound, new GroupFound());
    expect([f.name(), f.banner()]).toEqual(['Anteaters', 11]);

    b = new flatbuffers.Builder(32);
    const inv = open(finish(b, Message.GroupInvite, GroupInvite.createGroupInvite(b, 321)), Message.GroupInvite, new GroupInvite());
    expect(inv.targetId()).toBe(321);

    b = new flatbuffers.Builder(32);
    const ir = open(finish(b, Message.GroupInviteReply, GroupInviteReply.createGroupInviteReply(b, 70000, true)),
      Message.GroupInviteReply, new GroupInviteReply());
    expect([ir.groupId(), ir.accept()]).toEqual([70000, true]);

    b = new flatbuffers.Builder(32);
    const sr = open(finish(b, Message.GroupSetRank, GroupSetRank.createGroupSetRank(b, 99, true)),
      Message.GroupSetRank, new GroupSetRank());
    expect([sr.member(), sr.promote()]).toEqual([99, true]);

    b = new flatbuffers.Builder(32);
    expect(open(finish(b, Message.GroupKick, GroupKick.createGroupKick(b, 5)), Message.GroupKick, new GroupKick()).member()).toBe(5);

    b = new flatbuffers.Builder(32);
    expect(open(finish(b, Message.GroupHandover, GroupHandover.createGroupHandover(b, 6)), Message.GroupHandover, new GroupHandover()).member()).toBe(6);

    b = new flatbuffers.Builder(32);
    const sp = open(finish(b, Message.GroupSetPermission,
      GroupSetPermission.createGroupSetPermission(b, GroupRank.Officer, GroupPermission.Withdraw, true)),
      Message.GroupSetPermission, new GroupSetPermission());
    expect([sp.rank(), sp.permission(), sp.on()]).toEqual([GroupRank.Officer, GroupPermission.Withdraw, true]);

    b = new flatbuffers.Builder(16);
    GroupLeave.startGroupLeave(b);
    open(finish(b, Message.GroupLeave, GroupLeave.endGroupLeave(b)), Message.GroupLeave, new GroupLeave());
    b = new flatbuffers.Builder(16);
    GroupDisband.startGroupDisband(b);
    open(finish(b, Message.GroupDisband, GroupDisband.endGroupDisband(b)), Message.GroupDisband, new GroupDisband());
  });

  it('round-trips GroupInvited, GroupState and every GroupResult code', () => {
    let b = new flatbuffers.Builder(64);
    const inv = open(finish(b, Message.GroupInvited,
      GroupInvited.createGroupInvited(b, b.createString('Ann'), b.createString('Anteaters'), 12)),
      Message.GroupInvited, new GroupInvited());
    expect([inv.inviterName(), inv.groupName(), inv.groupId()]).toEqual(['Ann', 'Anteaters', 12]);

    b = new flatbuffers.Builder(256);
    const m1 = GroupMember.createGroupMember(b, 1, b.createString('Ann'), GroupRank.Leader, true);
    const m2 = GroupMember.createGroupMember(b, 2, b.createString('Bob'), GroupRank.Recruit, false);
    const members = GroupState.createMembersVector(b, [m1, m2]);
    GroupState.startPermissionsVector(b, 2);
    GroupPermissionSetting.createGroupPermissionSetting(b, GroupRank.Member, GroupPermission.VaultRoom, false);
    GroupPermissionSetting.createGroupPermissionSetting(b, GroupRank.Officer, GroupPermission.Keys, true);
    const perms = b.endVector();
    const name = b.createString('Anteaters');
    GroupState.startGroupState(b);
    GroupState.addGroupId(b, 12);
    GroupState.addName(b, name);
    GroupState.addBanner(b, 3);
    GroupState.addOwnRank(b, GroupRank.Leader);
    GroupState.addPermissions(b, perms);
    GroupState.addVaultGold(b, 2n ** 40n);
    GroupState.addHallId(b, 4);
    GroupState.addMembers(b, members);
    const s = open(finish(b, Message.GroupState, GroupState.endGroupState(b)), Message.GroupState, new GroupState());
    expect([s.groupId(), s.name(), s.banner(), s.ownRank(), s.vaultGold(), s.hallId(), s.membersLength(), s.permissionsLength()])
      .toEqual([12, 'Anteaters', 3, GroupRank.Leader, 2n ** 40n, 4, 2, 2]);
    expect([s.members(0)!.key(), s.members(0)!.name(), s.members(0)!.rank(), s.members(0)!.online()])
      .toEqual([1, 'Ann', GroupRank.Leader, true]);
    expect([s.members(1)!.key(), s.members(1)!.name(), s.members(1)!.rank(), s.members(1)!.online()])
      .toEqual([2, 'Bob', GroupRank.Recruit, false]);
    expect([s.permissions(0)!.rank(), s.permissions(0)!.permission(), s.permissions(0)!.on()])
      .toEqual([GroupRank.Officer, GroupPermission.Keys, true]);

    for (let code = GroupResultCode.Ok; code <= GroupResultCode.NotLeader; code++) {
      b = new flatbuffers.Builder(16);
      const r = open(finish(b, Message.GroupResult, GroupResult.createGroupResult(b, code)), Message.GroupResult, new GroupResult());
      expect(r.code()).toBe(code);
    }
  });

  it('decodes a chat message with no channel as local', () => {
    let b = new flatbuffers.Builder(64);
    const hi = b.createString('hi');
    ChatSend.startChatSend(b);
    ChatSend.addText(b, hi);
    let s = open(finish(b, Message.ChatSend, ChatSend.endChatSend(b)), Message.ChatSend, new ChatSend());
    expect(s.channel()).toBe(ChatChannel.Local);

    b = new flatbuffers.Builder(64);
    const hi2 = b.createString('hi');
    ChatMessage.startChatMessage(b);
    ChatMessage.addSpeakerId(b, 7);
    ChatMessage.addText(b, hi2);
    let c = open(finish(b, Message.ChatMessage, ChatMessage.endChatMessage(b)), Message.ChatMessage, new ChatMessage());
    expect([c.channel(), c.speakerName()]).toEqual([ChatChannel.Local, null]);

    b = new flatbuffers.Builder(64);
    const text = b.createString('team');
    ChatSend.startChatSend(b);
    ChatSend.addText(b, text);
    ChatSend.addChannel(b, ChatChannel.Group);
    s = open(finish(b, Message.ChatSend, ChatSend.endChatSend(b)), Message.ChatSend, new ChatSend());
    expect(s.channel()).toBe(ChatChannel.Group);

    b = new flatbuffers.Builder(64);
    const t2 = b.createString('team');
    const nm = b.createString('Ann');
    ChatMessage.startChatMessage(b);
    ChatMessage.addText(b, t2);
    ChatMessage.addChannel(b, ChatChannel.Group);
    ChatMessage.addSpeakerName(b, nm);
    c = open(finish(b, Message.ChatMessage, ChatMessage.endChatMessage(b)), Message.ChatMessage, new ChatMessage());
    expect([c.channel(), c.speakerName()]).toEqual([ChatChannel.Group, 'Ann']);
  });

  it('carries the group on EntityInfo', () => {
    let b = new flatbuffers.Builder(64);
    const gn = b.createString('Anteaters');
    EntityInfo.startEntityInfo(b);
    EntityInfo.addId(b, 5);
    EntityInfo.addGroupId(b, 12);
    EntityInfo.addGroupBanner(b, 9);
    EntityInfo.addGroupName(b, gn);
    let e = open(finish(b, Message.EntityInfo, EntityInfo.endEntityInfo(b)), Message.EntityInfo, new EntityInfo());
    expect([e.groupId(), e.groupBanner(), e.groupName()]).toEqual([12, 9, 'Anteaters']);

    b = new flatbuffers.Builder(64);
    EntityInfo.startEntityInfo(b);
    EntityInfo.addId(b, 5);
    e = open(finish(b, Message.EntityInfo, EntityInfo.endEntityInfo(b)), Message.EntityInfo, new EntityInfo());
    expect([e.groupId(), e.groupName()]).toEqual([0, null]);
  });
});
