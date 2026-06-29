import test from 'node:test';
import assert from 'node:assert';
import { FrameType, encodeFrame, decodeFrame, manualUtf8Encode, manualUtf8Decode } from './muxframe.js';

const enc = (s) => new TextEncoder().encode(s);

test('round-trip OPEN', () => {
  const f = { type: FrameType.Open, streamId: 1, method: '/CompanionService/Session', metadata: new Uint8Array() };
  const g = decodeFrame(encodeFrame(f));
  assert.equal(g.type, FrameType.Open);
  assert.equal(g.streamId, 1);
  assert.equal(g.method, '/CompanionService/Session');
});

test('round-trip MSG with byte segs', () => {
  const f = { type: FrameType.Msg, streamId: 3, json: enc('{"audio":{}}'), byteSegs: [new Uint8Array([1,2,3]), new Uint8Array([])] };
  const g = decodeFrame(encodeFrame(f));
  assert.equal(new TextDecoder().decode(g.json), '{"audio":{}}');
  assert.equal(g.byteSegs.length, 2);
  assert.deepEqual([...g.byteSegs[0]], [1,2,3]);
  assert.equal(g.byteSegs[1].length, 0);
});

test('round-trip END / streamId varint boundary', () => {
  const f = { type: FrameType.End, streamId: 300, code: 13, message: '繁忙' };
  const g = decodeFrame(encodeFrame(f));
  assert.equal(g.type, FrameType.End);
  assert.equal(g.streamId, 300);
  assert.equal(g.code, 13);
  assert.equal(g.message, '繁忙');
});

test('PING/PONG/HALF_CLOSE no body', () => {
  for (const t of [FrameType.Ping, FrameType.Pong, FrameType.HalfClose]) {
    const g = decodeFrame(encodeFrame({ type: t, streamId: 0 }));
    assert.equal(g.type, t);
  }
});

// 纯 JS UTF-8 回退(uni-app App 逻辑层无 TextEncoder/TextDecoder)须与原生一致并可往返。
test('manual UTF-8 matches native + round-trips (ascii/CJK/emoji)', () => {
  const samples = ['', 'hello', '黎明缠论', '势在人为', 'a你b好c', '🎉🚀', '混合 mix 中文 🎉 end', '{"audio":{},"text":"你好"}'];
  const nativeEnc = new TextEncoder();
  const nativeDec = new TextDecoder();
  for (const s of samples) {
    assert.deepEqual([...manualUtf8Encode(s)], [...nativeEnc.encode(s)], `encode mismatch: ${s}`);
    assert.equal(manualUtf8Decode(nativeEnc.encode(s)), s, `decode mismatch: ${s}`);
    assert.equal(manualUtf8Decode(manualUtf8Encode(s)), s, `round-trip mismatch: ${s}`);
    assert.equal(nativeDec.decode(manualUtf8Encode(s)), s, `native-decode(manual-encode) mismatch: ${s}`);
  }
});
