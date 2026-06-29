// muxframe —— 单 WS 多路复用线协议帧编解码(与后端 pkg/muxframe 同格式)。
// 帧 = [1B: version<<4|type][uvarint streamId][按类型 body]。长度前缀均 uvarint。
export const enum FrameType { Open = 1, Msg = 2, HalfClose = 3, End = 4, Reset = 5, Ping = 6, Pong = 7, TokenRefresh = 8 }
const VERSION = 1;

// UTF-8 编解码:优先用原生 TextEncoder/TextDecoder(浏览器/Node),缺失时(如 uni-app App
// 逻辑层 JS 引擎无 Web API)回退纯 JS 实现,保证全平台一致、零环境依赖。
/* global TextEncoder, TextDecoder */
const _te = typeof TextEncoder !== 'undefined' ? new TextEncoder() : null;
const _td = typeof TextDecoder !== 'undefined' ? new TextDecoder() : null;

export function manualUtf8Encode(str: string): Uint8Array {
  const bytes: number[] = [];
  for (let i = 0; i < str.length; i++) {
    let c = str.charCodeAt(i);
    if (c < 0x80) {
      bytes.push(c);
    } else if (c < 0x800) {
      bytes.push(0xc0 | (c >> 6), 0x80 | (c & 0x3f));
    } else if (c >= 0xd800 && c <= 0xdbff) {
      // 高代理:与后续低代理合成 4 字节码点
      const c2 = str.charCodeAt(++i);
      c = 0x10000 + ((c & 0x3ff) << 10) + (c2 & 0x3ff);
      bytes.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 0x3f), 0x80 | ((c >> 6) & 0x3f), 0x80 | (c & 0x3f));
    } else {
      bytes.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 0x3f), 0x80 | (c & 0x3f));
    }
  }
  return new Uint8Array(bytes);
}

export function manualUtf8Decode(bytes: Uint8Array): string {
  let str = '';
  for (let i = 0; i < bytes.length;) {
    const c = bytes[i++];
    if (c < 0x80) {
      str += String.fromCharCode(c);
    } else if (c < 0xe0) {
      str += String.fromCharCode(((c & 0x1f) << 6) | (bytes[i++] & 0x3f));
    } else if (c < 0xf0) {
      str += String.fromCharCode(((c & 0x0f) << 12) | ((bytes[i++] & 0x3f) << 6) | (bytes[i++] & 0x3f));
    } else {
      const cp = (((c & 0x07) << 18) | ((bytes[i++] & 0x3f) << 12) | ((bytes[i++] & 0x3f) << 6) | (bytes[i++] & 0x3f)) - 0x10000;
      str += String.fromCharCode(0xd800 + (cp >> 10), 0xdc00 + (cp & 0x3ff));
    }
  }
  return str;
}

export function utf8Encode(str: string): Uint8Array { return _te ? _te.encode(str) : manualUtf8Encode(str); }
export function utf8Decode(bytes: Uint8Array): string { return _td ? _td.decode(bytes) : manualUtf8Decode(bytes); }

export interface Frame {
  type: FrameType; streamId: number;
  method?: string; metadata?: Uint8Array;
  json?: Uint8Array; byteSegs?: Uint8Array[];
  code?: number; message?: string;
  token?: string;
}

class Writer {
  private parts: number[] = [];
  u8(b: number) { this.parts.push(b & 0xff); }
  uvarint(v: number) { let x = v >>> 0 === v ? v : v; while (x > 0x7f) { this.parts.push((x & 0x7f) | 0x80); x = Math.floor(x / 128); } this.parts.push(x & 0x7f); }
  bytes(b: Uint8Array) { this.uvarint(b.length); for (let i = 0; i < b.length; i++) this.parts.push(b[i]); }
  str(s: string) { this.bytes(utf8Encode(s)); }
  done(): Uint8Array { return new Uint8Array(this.parts); }
}

class Reader {
  constructor(private buf: Uint8Array, private pos = 0) {}
  u8(): number { return this.buf[this.pos++]; }
  uvarint(): number { let shift = 1, result = 0, b: number; do { b = this.buf[this.pos++]; result += (b & 0x7f) * shift; shift *= 128; } while (b & 0x80); return result; }
  bytes(): Uint8Array { const n = this.uvarint(); const out = this.buf.slice(this.pos, this.pos + n); this.pos += n; return out; }
  str(): string { return utf8Decode(this.bytes()); }
}

export function encodeFrame(f: Frame): Uint8Array {
  const w = new Writer();
  w.u8((VERSION << 4) | (f.type & 0x0f));
  w.uvarint(f.streamId);
  switch (f.type) {
    case FrameType.Open: w.str(f.method || ''); w.bytes(f.metadata || new Uint8Array()); break;
    case FrameType.Msg: {
      w.bytes(f.json || new Uint8Array());
      const segs = f.byteSegs || [];
      w.uvarint(segs.length);
      for (const s of segs) w.bytes(s);
      break;
    }
    case FrameType.End: case FrameType.Reset: w.uvarint(f.code || 0); w.str(f.message || ''); break;
    case FrameType.TokenRefresh: w.str(f.token || ''); break;
    // HalfClose / Ping / Pong: 无 body
  }
  return w.done();
}

export function decodeFrame(buf: Uint8Array): Frame {
  const h = buf[0];
  if (h >> 4 !== VERSION) throw new Error('muxframe: bad version');
  const r = new Reader(buf, 1);
  const f: Frame = { type: (h & 0x0f) as FrameType, streamId: r.uvarint() };
  switch (f.type) {
    case FrameType.Open: f.method = r.str(); f.metadata = r.bytes(); break;
    case FrameType.Msg: {
      f.json = r.bytes();
      const n = r.uvarint(); f.byteSegs = [];
      for (let i = 0; i < n; i++) f.byteSegs.push(r.bytes());
      break;
    }
    case FrameType.End: case FrameType.Reset: f.code = r.uvarint(); f.message = r.str(); break;
    case FrameType.TokenRefresh: f.token = r.str(); break;
  }
  return f;
}
