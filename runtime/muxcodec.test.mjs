import test from 'node:test';
import assert from 'node:assert';
import { deflate, inflate } from './muxcodec.js';

test('deflate/inflate audio.data', () => {
  const raw = new Uint8Array([1,2,3,255]);
  const obj = { audio: { data: raw } };
  const { json, segs } = deflate(obj, [['audio','data']]);
  const text = new TextDecoder().decode(json);
  assert.ok(!text.includes('1,2,3'), 'JSON 不应含裸数组');
  assert.equal(segs.length, 1);
  assert.deepEqual([...segs[0]], [1,2,3,255]);
  const back = inflate(json, segs, [['audio','data']]);
  assert.deepEqual([...back.audio.data], [1,2,3,255]);
});

test('no bytes path', () => {
  const { json, segs } = deflate({ delta: { text: '你好' } }, []);
  assert.equal(segs.length, 0);
  const back = inflate(json, segs, []);
  assert.equal(back.delta.text, '你好');
});

// 回归:bytes 路径已声明(audio.data),但本帧选的是另一个 oneof 成员(say/listen)。
// deflate 不得创建 audio,inflate 不得凭空补出 audio.data——否则服务端 protojson 会因
// 「oneof 已置位」整帧解析失败、被静默丢弃(在线体验:文字不回复、语音不启动)。
test('oneof sibling: 文字帧不得注入 audio.data', () => {
  const paths = [['audio', 'data']];
  const { json, segs } = deflate({ say: { text: 'hi' } }, paths);
  const obj = JSON.parse(new TextDecoder().decode(json));
  assert.deepEqual(obj, { say: { text: 'hi' } }, 'deflate 不应创建 audio');
  const back = inflate(json, segs, paths);
  assert.equal(back.say.text, 'hi');
  assert.ok(!('audio' in back), 'inflate 不应凭空补出 audio');
});

// audio 帧:deflate 置空占位(保留键)、inflate 据键存在回填,字节须无损往返。
test('audio 帧字节无损往返(置空占位语义)', () => {
  const paths = [['audio', 'data']];
  const raw = new Uint8Array([10, 11, 12]);
  const { json, segs } = deflate({ audio: { data: raw } }, paths);
  const obj = JSON.parse(new TextDecoder().decode(json));
  assert.equal(obj.audio.data, '', 'deflate 应把叶子置成空串占位(保留键)');
  const back = inflate(json, segs, paths);
  assert.deepEqual([...back.audio.data], [10, 11, 12]);
});
