import { Emote } from '../protocol';
import { encodeEmote } from '../net/codec';

/** Sends one EmoteSend(Wave) through the session. */
export function sendWave(session: { send(bytes: Uint8Array): void }): void {
  session.send(encodeEmote(Emote.Wave));
}
