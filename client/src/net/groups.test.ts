import * as flatbuffers from 'flatbuffers';
import { describe, expect, it } from 'vitest';
import {
  ChatChannel, ChatMessage, Envelope, EntityInfo, EntityKind, GroupInvited, GroupMember, GroupPermission, GroupPermissionSetting,
  GroupRank, GroupResult, GroupResultCode, GroupState, Message,
} from '../protocol';
import { GameState } from '../state/world';
import { groupResultText } from '../state/groups';
import {
  decode, encodeChat, encodeGroupDisband, encodeGroupFound, encodeGroupHandover, encodeGroupInvite, encodeGroupInviteReply,
  encodeGroupKick, encodeGroupLeave, encodeGroupSetPermission, encodeGroupSetRank,
} from './codec';
import { GroupDisband, GroupFound, GroupHandover, GroupInvite, GroupInviteReply, GroupKick, GroupLeave, GroupSetPermission, GroupSetRank, ChatSend } from '../protocol';

function finish(b: flatbuffers.Builder, type: Message, msg: flatbuffers.Offset): Uint8Array {
  b.finish(Envelope.createEnvelope(b, type, msg));
  return b.asUint8Array().slice();
}

function open<T>(bytes: Uint8Array, type: Message, msg: T & { __init(i: number, bb: flatbuffers.ByteBuffer): T }): T {
  const env = Envelope.getRootAsEnvelope(new flatbuffers.ByteBuffer(bytes));
  expect(env.msgType()).toBe(type);
  return env.msg(msg as never) as T;
}

function stateBytes(): Uint8Array {
  const b = new flatbuffers.Builder(256);
  const name = b.createString('Anteaters');
  const m1 = GroupMember.createGroupMember(b, 11, b.createString('Peter'), GroupRank.Leader, true);
  const m2 = GroupMember.createGroupMember(b, 12, b.createString('Zot'), GroupRank.Recruit, false);
  GroupState.startPermissionsVector(b, 2);
  GroupPermissionSetting.createGroupPermissionSetting(b, GroupRank.Member, GroupPermission.Withdraw, false);
  GroupPermissionSetting.createGroupPermissionSetting(b, GroupRank.Officer, GroupPermission.Keys, true);
  const perms = b.endVector();
  const msg = GroupState.createGroupState(b, 5, name, 3, GroupRank.Officer, perms, 1234n, 7, GroupState.createMembersVector(b, [m1, m2]));
  return finish(b, Message.GroupState, msg);
}

describe('group messages', () => {
  it('GroupState, GroupInvited and GroupResult decode and fill the group store', () => {
    const state = decode(stateBytes());
    expect(state).toMatchObject({ type: 'groupState', groupId: 5, name: 'Anteaters', banner: 3, ownRank: GroupRank.Officer, vaultGold: 1234, hallId: 7 });
    const w = new GameState();
    w.handle(state!, 0);
    expect(w.groups.inGroup).toBe(true);
    expect(w.groups.group.members).toEqual([
      { key: 11, name: 'Peter', rank: GroupRank.Leader, online: true },
      { key: 12, name: 'Zot', rank: GroupRank.Recruit, online: false },
    ]);
    expect(w.groups.group.permissions).toEqual([
      { rank: GroupRank.Officer, permission: GroupPermission.Keys, on: true },
      { rank: GroupRank.Member, permission: GroupPermission.Withdraw, on: false },
    ]);

    const b = new flatbuffers.Builder(64);
    const inv = decode(finish(b, Message.GroupInvited, GroupInvited.createGroupInvited(b, b.createString('Ann'), b.createString('Bears'), 9)));
    expect(inv).toEqual({ type: 'groupInvited', inviterName: 'Ann', groupName: 'Bears', groupId: 9 });
    w.handle(inv!, 0);
    expect(w.groups.invite).toEqual({ inviterName: 'Ann', groupName: 'Bears', groupId: 9 });

    const b2 = new flatbuffers.Builder(16);
    const res = decode(finish(b2, Message.GroupResult, GroupResult.createGroupResult(b2, GroupResultCode.NameTaken)));
    expect(res).toEqual({ type: 'groupResult', code: GroupResultCode.NameTaken });
    w.handle(res!, 0);
    expect(w.chat.log.at(-1)).toMatchObject({ system: true, text: groupResultText(GroupResultCode.NameTaken) });

    const b3 = new flatbuffers.Builder(16);
    w.handle(decode(finish(b3, Message.GroupState, GroupState.createGroupState(b3)))!, 0);
    expect(w.groups.inGroup).toBe(false);
    expect(w.groups.group.members).toEqual([]);
  });

  it('senders encode messages that decode to the same fields', () => {
    const f = open(encodeGroupFound('Anteaters', 4), Message.GroupFound, new GroupFound());
    expect([f.name(), f.banner()]).toEqual(['Anteaters', 4]);
    expect(open(encodeGroupInvite(300), Message.GroupInvite, new GroupInvite()).targetId()).toBe(300);
    const r = open(encodeGroupInviteReply(9, true), Message.GroupInviteReply, new GroupInviteReply());
    expect([r.groupId(), r.accept()]).toEqual([9, true]);
    const s = open(encodeGroupSetRank(12, false), Message.GroupSetRank, new GroupSetRank());
    expect([s.member(), s.promote()]).toEqual([12, false]);
    const p = open(encodeGroupSetRank(13, true), Message.GroupSetRank, new GroupSetRank());
    expect([p.member(), p.promote()]).toEqual([13, true]);
    expect(open(encodeGroupKick(14), Message.GroupKick, new GroupKick()).member()).toBe(14);
    open(encodeGroupLeave(), Message.GroupLeave, new GroupLeave());
    expect(open(encodeGroupHandover(15), Message.GroupHandover, new GroupHandover()).member()).toBe(15);
    const sp = open(encodeGroupSetPermission(GroupRank.Officer, GroupPermission.VaultRoom, true), Message.GroupSetPermission, new GroupSetPermission());
    expect([sp.rank(), sp.permission(), sp.on()]).toEqual([GroupRank.Officer, GroupPermission.VaultRoom, true]);
    open(encodeGroupDisband(), Message.GroupDisband, new GroupDisband());
    const c = open(encodeChat('hi', ChatChannel.Group), Message.ChatSend, new ChatSend());
    expect([c.text(), c.channel()]).toEqual(['hi', ChatChannel.Group]);
  });

  it('EntityInfo group id, banner and name reach the entity state', () => {
    const b = new flatbuffers.Builder(64);
    const msg = EntityInfo.createEntityInfo(b, 7, EntityKind.Player, 0, 5, 0, 0, 0, 0, 20, 5, 3, b.createString('Anteaters'));
    const w = new GameState();
    w.handle(decode(finish(b, Message.EntityInfo, msg))!, 0);
    expect(w.items.entityInfo(7)).toMatchObject({ groupId: 5, groupBanner: 3, groupName: 'Anteaters' });
  });

  it('has a readable line for every GroupResult code', () => {
    for (const code of Object.values(GroupResultCode).filter((v): v is GroupResultCode => typeof v === 'number')) {
      if (code === GroupResultCode.Ok) continue;
      expect(groupResultText(code).length, `code ${code}`).toBeGreaterThan(5);
    }
  });

  it('group chat goes to the log with a marker and never overhead; local chat is unchanged', () => {
    const w = new GameState();
    const chat = (speakerId: number, text: string, channel: ChatChannel, name: string): Uint8Array => {
      const b = new flatbuffers.Builder(64);
      return finish(b, Message.ChatMessage, ChatMessage.createChatMessage(b, speakerId, b.createString(text), channel, b.createString(name)));
    };
    w.handle(decode(chat(7, 'rally', ChatChannel.Group, 'Zot'))!, 100);
    expect(w.chat.log.at(-1)).toEqual({ speakerId: 7, text: 'rally', group: true, speakerName: 'Zot' });
    expect(w.chat.overhead(7, 100)).toBeNull();
    w.handle(decode(chat(8, 'hello', ChatChannel.Local, ''))!, 100);
    expect(w.chat.log.at(-1)).toEqual({ speakerId: 8, text: 'hello' });
    expect(w.chat.overhead(8, 100)?.text).toBe('hello');
  });
});
