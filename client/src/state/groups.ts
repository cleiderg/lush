import { GroupResultCode } from '../protocol';
import type { GroupInvitedView, GroupStateView } from '../net/codec';

export type { GroupStateView };

const NO_GROUP: GroupStateView = {
  groupId: 0, name: '', banner: 0, ownRank: 0, permissions: [], vaultGold: 0, hallId: 0, members: [],
};

/** The player's group (GroupState) and the pending invitation (GroupInvited). */
export class GroupStore {
  private current: GroupStateView = NO_GROUP;
  private pendingInvite: GroupInvitedView | null = null;
  private listeners = new Set<() => void>();

  /** The group; groupId 0 and no members when not in a group. */
  get group(): GroupStateView {
    return this.current;
  }

  get inGroup(): boolean {
    return this.current.groupId !== 0;
  }

  /** The latest invitation not yet answered, or null. */
  get invite(): GroupInvitedView | null {
    return this.pendingInvite;
  }

  subscribe(fn: () => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  setState(s: GroupStateView): void {
    const { groupId, name, banner, ownRank, vaultGold, hallId } = s;
    this.current = {
      groupId, name, banner, ownRank, vaultGold, hallId,
      permissions: s.permissions.map((p) => ({ ...p })),
      members: s.members.map((m) => ({ ...m })),
    };
    // Joining the invited group answers the invitation.
    if (this.pendingInvite && this.pendingInvite.groupId === groupId) this.pendingInvite = null;
    this.notify();
  }

  setInvite(i: GroupInvitedView): void {
    this.pendingInvite = { inviterName: i.inviterName, groupName: i.groupName, groupId: i.groupId };
    this.notify();
  }

  clearInvite(): void {
    if (!this.pendingInvite) return;
    this.pendingInvite = null;
    this.notify();
  }

  clear(): void {
    this.current = NO_GROUP;
    this.pendingInvite = null;
    this.notify();
  }

  private notify(): void {
    for (const fn of this.listeners) fn();
  }
}

export function groupInvitedText(inviterName: string, groupName: string): string {
  return `${inviterName} invites you to join ${groupName}.`;
}

const RESULT_TEXT: Record<GroupResultCode, string> = {
  [GroupResultCode.Ok]: '',
  [GroupResultCode.NotInGroup]: "You're not in a group.",
  [GroupResultCode.AlreadyInGroup]: "You're already in a group.",
  [GroupResultCode.NameInvalid]: 'A group name has 3 to 20 letters, numbers and spaces.',
  [GroupResultCode.NameBlocked]: "That name isn't allowed.",
  [GroupResultCode.NameTaken]: 'That group name is taken.',
  [GroupResultCode.BannerInvalid]: 'Choose one of the banners.',
  [GroupResultCode.NotEnoughGold]: "You don't have enough gold.",
  [GroupResultCode.NoPermission]: "Your rank isn't high enough to do that.",
  [GroupResultCode.TargetNotFound]: "You can't find that player.",
  [GroupResultCode.TargetInGroup]: 'That player is already in a group.',
  [GroupResultCode.TargetCoolingDown]: 'That player left a group recently and cannot join one yet.',
  [GroupResultCode.CoolingDown]: 'You left a group recently. You cannot join one yet.',
  [GroupResultCode.GroupFull]: 'The group is full.',
  [GroupResultCode.InviteExpired]: 'That invitation has expired.',
  [GroupResultCode.RankTooHigh]: "That member's rank isn't below yours.",
  [GroupResultCode.CannotPromote]: "You can't promote that member any higher.",
  [GroupResultCode.CannotDemote]: "That member can't be demoted any lower.",
  [GroupResultCode.LeaderMustHandover]: 'Hand over leadership before you leave.',
  [GroupResultCode.HandoverNotOfficer]: 'Leadership can only pass to an Officer.',
  [GroupResultCode.NotLeader]: 'Only the leader can do that.',
};

/** The chat-log line for a GroupResult; empty for Ok (nothing to say) and for unknown codes. */
export function groupResultText(code: GroupResultCode): string {
  return RESULT_TEXT[code] ?? '';
}
