// muxcodec —— 业务对象 ⇄ (去掉 bytes 字段的 JSON 字节, 裸字节段[])。option 2:bytes 不进 JSON、裸字节另带。
import { utf8Encode, utf8Decode } from './muxframe';

// structuredCloneSafe:深拷贝但保留 Uint8Array 引用(JSON.stringify 会把它变成 {"0":..}, 故先把 bytes 摘掉再 stringify)。
function structuredCloneSafe(o: any): any {
  if (o instanceof Uint8Array) return o;
  if (Array.isArray(o)) return o.map(structuredCloneSafe);
  if (o && typeof o === 'object') { const r: any = {}; for (const k of Object.keys(o)) r[k] = structuredCloneSafe(o[k]); return r; }
  return o;
}

// takeAtPath:抠出 bytes 叶子的值作为裸字节段,并把该叶子「置空串占位」(保留键、不 delete)。
// 关键:键是否存在 = 对端 inflate 是否回填的唯一信号。字段本就不存在(典型:bytes 属于某个 oneof,
// 而本帧选的是别的成员)时,绝不创建它——否则回填端会凭空多出一个字段,撞坏 oneof / 污染消息。
// 与服务端 Deflate(takeBase64AtPath 同样「置空不删除、缺失不创建」)严格对称。
function takeAtPath(obj: any, path: string[]): Uint8Array {
  let cur = obj;
  for (let i = 0; i < path.length - 1; i++) {
    if (cur == null || typeof cur !== 'object') return new Uint8Array();
    cur = cur[path[i]];
  }
  if (cur == null || typeof cur !== 'object') return new Uint8Array();
  const key = path[path.length - 1];
  if (!(key in cur)) return new Uint8Array();
  const v = cur[key];
  cur[key] = ''; // 留空串占位,标记「该字段确实出现过」
  if (v instanceof Uint8Array) return v;
  if (Array.isArray(v)) return new Uint8Array(v);
  return new Uint8Array();
}

// setAtPath:仅当该 bytes 路径(每层中间对象 + 叶子键)本就存在时,才把裸字节段回填进去。
// 任一层缺失即跳过、绝不创建——这正是「只还原 deflate 当初抠空过的字段」,与服务端 Inflate 对称。
// 反例(必须避免):给一个 {say:{...}} 的消息凭空补出 audio:{data:..},会让 oneof 多出第二个成员。
function setAtPath(obj: any, path: string[], val: Uint8Array): void {
  let cur = obj;
  for (let i = 0; i < path.length - 1; i++) {
    if (cur == null || typeof cur !== 'object' || !(path[i] in cur)) return;
    cur = cur[path[i]];
  }
  if (cur == null || typeof cur !== 'object') return;
  const key = path[path.length - 1];
  if (!(key in cur)) return;
  cur[key] = val;
}

export function deflate(obj: any, bytesPaths: string[][]): { json: Uint8Array; segs: Uint8Array[] } {
  if (!bytesPaths || bytesPaths.length === 0) {
    return { json: utf8Encode(JSON.stringify(obj ?? {})), segs: [] };
  }
  const work = structuredCloneSafe(obj ?? {});
  const segs: Uint8Array[] = [];
  for (const p of bytesPaths) segs.push(takeAtPath(work, p));
  return { json: utf8Encode(JSON.stringify(work)), segs };
}

export function inflate(json: Uint8Array, segs: Uint8Array[], bytesPaths: string[][]): any {
  const obj = JSON.parse(utf8Decode(json));
  if (bytesPaths) for (let i = 0; i < bytesPaths.length; i++) setAtPath(obj, bytesPaths[i], segs[i] || new Uint8Array());
  return obj;
}
