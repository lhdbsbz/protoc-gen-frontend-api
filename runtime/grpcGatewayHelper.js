import * as baseServiceModule from 'BASE_SERVICE_IMPORT_PLACEHOLDER';
// 循环依赖：request.js 反过来也 import 本模块（default），所以这里不能在模块求值阶段读取它的 default
// （此时 request.js 的 export default 可能尚未执行 → TDZ "Cannot access 'default' before initialization"）。
// 改为惰性读取：下面所有用到 baseService 的地方都在函数运行时才调用，那时 request.js 已初始化完毕。
const getBaseService = () => baseServiceModule.default || baseServiceModule;

// 多路复用帧编解码与 bytes 字段分离器（由插件一同写入同目录）
import { FrameType, encodeFrame, decodeFrame } from './muxframe.js';
import { deflate, inflate } from './muxcodec.js';

// ==================== UTF-8 Polyfill ====================
function uint8ArrayToString(arr) {
    let str = '';
    for (let i = 0; i < arr.length; i++) {
        const code = arr[i];
        if (code < 0x80) {
            str += String.fromCharCode(code);
        } else if (code < 0xe0) {
            str += String.fromCharCode(((code & 0x1f) << 6) | (arr[++i] & 0x3f));
        } else if (code < 0xf0) {
            str += String.fromCharCode(
                ((code & 0x0f) << 12) | ((arr[++i] & 0x3f) << 6) | (arr[++i] & 0x3f)
            );
        } else {
            const utf32 =
                ((code & 0x07) << 18) |
                ((arr[++i] & 0x3f) << 12) |
                ((arr[++i] & 0x3f) << 6) |
                (arr[++i] & 0x3f);
            const offset = utf32 - 0x10000;
            str += String.fromCharCode(0xd800 + (offset >> 10), 0xdc00 + (offset & 0x3ff));
        }
    }
    return str;
}

let activeAdapter = {};

// ==================== bytes ↔ base64 自动编解码 ====================
// 后端 protojson 把 proto 的 bytes 字段编码为 base64 字符串。生成器按响应/请求消息的
// 描述符算出 bytes 字段路径（reqPaths/respPaths），这里据此在出入站自动转换，
// 业务层从此双向只面对 Uint8Array。多端通用：优先 atob/btoa，无则走手写 base64 表。
const B64_CHARS = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/';

function bytesToBase64(bytes) {
    if (typeof btoa === 'function') {
        let binary = '';
        for (let i = 0; i < bytes.length; i++) binary += String.fromCharCode(bytes[i]);
        return btoa(binary);
    }
    let out = '';
    for (let i = 0; i < bytes.length; i += 3) {
        const b0 = bytes[i];
        const b1 = i + 1 < bytes.length ? bytes[i + 1] : 0;
        const b2 = i + 2 < bytes.length ? bytes[i + 2] : 0;
        out += B64_CHARS[b0 >> 2];
        out += B64_CHARS[((b0 & 3) << 4) | (b1 >> 4)];
        out += i + 1 < bytes.length ? B64_CHARS[((b1 & 15) << 2) | (b2 >> 6)] : '=';
        out += i + 2 < bytes.length ? B64_CHARS[b2 & 63] : '=';
    }
    return out;
}

function base64ToBytes(b64) {
    if (typeof atob === 'function') {
        const binary = atob(b64);
        const bytes = new Uint8Array(binary.length);
        for (let i = 0; i < binary.length; i++) bytes[i] = binary.charCodeAt(i);
        return bytes;
    }
    const clean = b64.replace(/[^A-Za-z0-9+/]/g, '');
    const outLen = Math.floor((clean.length * 3) / 4);
    const bytes = new Uint8Array(outLen);
    let p = 0;
    for (let i = 0; i < clean.length; i += 4) {
        const e0 = B64_CHARS.indexOf(clean[i]);
        const e1 = B64_CHARS.indexOf(clean[i + 1]);
        const e2 = B64_CHARS.indexOf(clean[i + 2]);
        const e3 = B64_CHARS.indexOf(clean[i + 3]);
        const n = (e0 << 18) | (e1 << 12) | ((e2 & 63) << 6) | (e3 & 63);
        if (p < outLen) bytes[p++] = (n >> 16) & 0xff;
        if (e2 !== -1 && p < outLen) bytes[p++] = (n >> 8) & 0xff;
        if (e3 !== -1 && p < outLen) bytes[p++] = n & 0xff;
    }
    return bytes;
}

// 叶子转换：bytes 字段的值可能是单个、数组（repeated）或 map（map<_,bytes>）。
// 解码 base64 串→Uint8Array；编码 Uint8Array→base64 串。先于 object 分支判类型化数组。
function decodeLeaf(v) {
    if (typeof v === 'string') return base64ToBytes(v);
    if (Array.isArray(v)) return v.map(decodeLeaf);
    if (v && typeof v === 'object') {
        for (const k in v) v[k] = decodeLeaf(v[k]);
        return v;
    }
    return v;
}

function encodeLeaf(v) {
    if (v instanceof Uint8Array) return bytesToBase64(v);
    if (v instanceof ArrayBuffer) return bytesToBase64(new Uint8Array(v));
    if (Array.isArray(v)) return v.map(encodeLeaf);
    if (v && typeof v === 'object') {
        for (const k in v) v[k] = encodeLeaf(v[k]);
        return v;
    }
    return v;
}

// 按 key 路径就地改写 obj 上的 bytes 字段。中间层遇数组（repeated message）自动逐元素递归。
function walkBytesPath(node, path, idx, leaf) {
    if (node == null || typeof node !== 'object') return;
    if (Array.isArray(node)) {
        for (const el of node) walkBytesPath(el, path, idx, leaf);
        return;
    }
    const key = path[idx];
    if (idx === path.length - 1) {
        if (key in node) node[key] = leaf(node[key]);
    } else {
        walkBytesPath(node[key], path, idx + 1, leaf);
    }
}

// paths: string[][]（每条是一组 key）。obj 为空或 paths 为空时原样返回。
function applyBytesPaths(obj, paths, leaf) {
    if (!obj || !paths || !paths.length) return obj;
    for (const path of paths) walkBytesPath(obj, path, 0, leaf);
    return obj;
}


export function configureGrpcWeb(adapter) {
    activeAdapter = adapter;
}

function getSessionToken() {
    try {
        const token = localStorage.getItem('x-session-id');
        if (token) return token;
    } catch {}
    try {
        if (typeof uni !== 'undefined' && uni.getStorageSync) {
            const raw = uni.getStorageSync('user');
            if (raw) {
                const parsed = typeof raw === 'string' ? JSON.parse(raw) : raw;
                if (parsed && parsed.token) return parsed.token;
            }
        }
    } catch {}
    return '';
}

// ==================== Standard Browser Stream ====================
function defaultRequestStream(url, data, opts = {}) {
    const baseService = getBaseService();
    const bodyStr = JSON.stringify(data);
    const headers = { 'Content-Type': 'application/json' };

    const buildHeadersFn = baseService.buildHeaders || baseService.getHeaders;
    const finalHeaders = buildHeadersFn ? buildHeadersFn(bodyStr) : headers;

    const baseUrl = baseService.baseUrl || baseService.BASE_URL || "";
    const finalUrl = url.startsWith('http') ? url : (baseUrl ? (baseUrl.endsWith('/') ? baseUrl.slice(0, -1) : baseUrl) + url : url);

    return new Promise((resolve, reject) => {
        fetch(finalUrl, {
            method: 'POST',
            headers: finalHeaders,
            body: bodyStr
        }).then(async (response) => {
            if (response.status !== 200) {
                throw new Error("HTTP error! status: " + response.status);
            }
            const reader = response.body.getReader();
            let buffer = '';

            try {
                while (true) {
                    const { done, value } = await reader.read();
                    if (done) break;

                    buffer += uint8ArrayToString(value);
                    const lines = buffer.split('\n');
                    buffer = lines.pop();

                    for (const line of lines) {
                        if (line.trim() === '') continue;
                        try {
                            const parsed = JSON.parse(line);
                            if (parsed.error) {
                                const err = new Error(parsed.error.message);
                                opts.onError?.(err);
                                reject(err);
                                return;
                            }
                            opts.onChunk?.(parsed.result);
                        } catch (e) {
                            console.error("Failed to parse stream JSON line:", line, e);
                        }
                    }
                }
                opts.onSuccess?.();
                resolve();
            } catch (err) {
                opts.onError?.(err);
                reject(err);
            }
        }).catch((err) => {
            opts.onError?.(err);
            reject(err);
        });
    });
}

// ==================== Standard Browser WebSocket ====================
class BrowserWebSocketWrapper {
    constructor(url, protocols) {
        this.ws = new WebSocket(url, protocols);
        this.ws.binaryType = 'arraybuffer';
        this.ws.onopen = () => this.onopen?.();
        this.ws.onmessage = (ev) => {
            if (ev.data instanceof ArrayBuffer) {
                this.onmessage?.({ data: ev.data });
            }
        };
        this.ws.onerror = (err) => this.onerror?.(err);
        this.ws.onclose = () => this.onclose?.();
    }
    send(data) {
        this.ws.send(data);
    }
    close() {
        this.ws.close();
    }
}

// ==================== MuxConnection 单例 + StreamHandle ====================
// 一条共享 WS（/_mux）多路复用所有逻辑流，取代原来"每次调用一条 WS"的做法。

/** 发送队列上限：超出时丢弃最旧的帧并告警（客户端背压极少触顶） */
const SEND_QUEUE_CAP = 1024;

/** 心跳间隔（毫秒） */
const HEARTBEAT_INTERVAL_MS = 30000;

/** 指数退避重连延迟初始值与上限 */
const RECONNECT_BASE_MS = 1000;
const RECONNECT_MAX_MS = 30000;

/** 每条逻辑流的公共句柄，调用方依赖此接口（不改变签名）。 */
class StreamHandle {
    constructor(id, mux, opts, reqPaths, respPaths) {
        this.id = id;
        this.mux = mux;
        this.opts = opts;
        this.reqPaths = reqPaths;
        this.respPaths = respPaths;
        this.ended = false;
    }

    /** 连接已建立且本流未结束 */
    get isOpened() {
        return this.mux.isOpen && !this.ended;
    }

    /** 发送业务消息：deflate(bytes 字段分离) → 入队 MSG 帧 */
    send(msg) {
        if (this.ended) return;
        const { json, segs } = deflate(msg, this.reqPaths || []);
        this.mux.enqueue(encodeFrame({ type: FrameType.Msg, streamId: this.id, json, byteSegs: segs }));
    }

    /** 半关闭（客户端发送完毕，等待服务端响应） */
    halfClose() {
        if (this.ended) return;
        this.mux.enqueue(encodeFrame({ type: FrameType.HalfClose, streamId: this.id }));
    }

    /** 主动关闭：发 RESET 帧并本地清理 */
    close() {
        if (this.ended) return;
        this.ended = true;
        this.mux.enqueue(encodeFrame({ type: FrameType.Reset, streamId: this.id }));
        this.mux.removeStream(this.id);
    }

    /** 收到服务端 MSG 帧时调用，inflate 后转交业务回调 */
    onMsg(json, byteSegs) {
        try {
            const obj = inflate(json, byteSegs, this.respPaths || []);
            this.opts.onMessage?.(obj);
        } catch (e) {
            console.error('MuxConnection: inflate MSG 失败', e);
        }
    }

    /** 收到 END 帧时调用（code=0 → onClose，否则 onError） */
    onEnd(code, message) {
        if (this.ended) return;
        this.ended = true;
        if (code === 0) {
            this.opts.onClose?.();
        } else {
            this.opts.onError?.(new Error(message || '流结束，code=' + code));
        }
    }

    /** 收到 RESET 帧时调用 */
    onReset(message) {
        if (this.ended) return;
        this.ended = true;
        this.opts.onError?.(new Error(message || '流被重置'));
    }

    /** socket open 时触发 onOpen 回调（并在有 _initData 时发送初始消息） */
    fireOpen(initData) {
        this.opts.onOpen?.();
        if (initData && typeof initData === 'object' && Object.keys(initData).length > 0) {
            this.send(initData);
        }
    }
}

/**
 * 模块级单例多路复用 WS 连接。
 * 持一条 WS 到 /<wsBase>/grpc-web-websocket/_mux，内部维护 streamId→StreamHandle 映射。
 */
class MuxConnection {
    constructor() {
        this.socket = null;
        /** 当前 socket 已 open */
        this.isOpen = false;
        this.streams = new Map();
        this.nextId = 0;
        /** 未发出的已编码帧（socket 尚未 open 时暂存） */
        this.sendQueue = [];
        /** socket open 时待触发的 {handle, initData} 列表 */
        this.pendingOpens = [];
        this.heartbeatTimer = null;
        this.reconnectTimer = null;
        this.reconnectCount = 0;
        /** 是否已被显式销毁（不再重连） */
        this.destroyed = false;
    }

    /** 分配下一个流 ID，建/复用连接，返回 StreamHandle */
    openStream(path, opts) {
        const id = ++this.nextId;
        const method = path.replace(/^\/grpc-web-websocket/, '');
        const handle = new StreamHandle(id, this, opts, opts._reqBytesPaths, opts._respBytesPaths);
        this.streams.set(id, handle);

        // 入队 OPEN 帧
        this.enqueue(encodeFrame({ type: FrameType.Open, streamId: id, method, metadata: new Uint8Array() }));

        // 建连（若尚未建立）
        if (!this.socket) {
            this._connect();
        }

        if (this.isOpen) {
            // socket 已 open：同步触发 onOpen（乐观路径）
            handle.fireOpen(opts._initData);
        } else {
            // socket 未 open：open 后再触发
            this.pendingOpens.push({ handle, initData: opts._initData });
        }

        return handle;
    }

    /** 入队一个已编码帧；超出上限时丢弃最旧帧并告警 */
    enqueue(frame) {
        if (this.isOpen && this.socket) {
            // socket 已 open，直接发送
            this.socket.send(frame.buffer);
        } else {
            if (this.sendQueue.length >= SEND_QUEUE_CAP) {
                console.warn('MuxConnection: sendQueue 已满，丢弃最旧帧');
                this.sendQueue.shift();
            }
            this.sendQueue.push(frame);
        }
    }

    /** 从 streams 表移除（由 StreamHandle.close / onEnd / onReset 调用） */
    removeStream(id) {
        this.streams.delete(id);
    }

    /** 构建 WS URL，与旧 GrpcWebWebSocketClient 逻辑一致，末尾固定为 /_mux */
    _buildWsUrl() {
        const key1 = 'BASE_URL';
        const key2 = 'baseUrl';
        const baseService = getBaseService();
        const baseUrl = baseServiceModule[key1] || baseServiceModule[key2] || baseService[key2] || baseService[key1] || '';
        const path = '/grpc-web-websocket/_mux';
        const finalUrl = baseUrl ? (baseUrl.endsWith('/') ? baseUrl.slice(0, -1) : baseUrl) + path : path;
        let wsUrl = finalUrl.replace(/^http/, 'ws');
        if (wsUrl.startsWith('/')) {
            if (typeof window !== 'undefined' && window.location) {
                const origin = window.location.origin.replace(/^http/, 'ws');
                wsUrl = origin + wsUrl;
            }
        }
        return wsUrl;
    }

    /** 建立 WS 连接（重连时同样调用） */
    _connect() {
        if (this.destroyed || this.socket) return;

        const wsUrl = this._buildWsUrl();

        // session token 走 WS 子协议（与旧实现一致）
        const subprotocols = ['grpc-websockets'];
        const token = getSessionToken();
        if (token) subprotocols.push('lmcl.bearer.' + token);

        const creator = activeAdapter.createWebSocket || ((u, p) => new BrowserWebSocketWrapper(u, p));
        try {
            this.socket = creator(wsUrl, subprotocols);
        } catch (err) {
            console.error('MuxConnection: 创建 socket 失败', err);
            this._scheduleReconnect();
            return;
        }

        this.socket.onopen = () => {
            this.isOpen = true;
            this.reconnectCount = 0;

            // 刷新 sendQueue
            for (const frame of this.sendQueue) {
                this.socket.send(frame.buffer);
            }
            this.sendQueue = [];

            // 触发待 open 的流
            for (const { handle, initData } of this.pendingOpens) {
                handle.fireOpen(initData);
            }
            this.pendingOpens = [];

            // 启动心跳
            this._startHeartbeat();
        };

        this.socket.onmessage = (event) => {
            try {
                const frame = decodeFrame(new Uint8Array(event.data));
                const handle = this.streams.get(frame.streamId);

                switch (frame.type) {
                    case FrameType.Msg:
                        handle?.onMsg(frame.json || new Uint8Array(), frame.byteSegs || []);
                        break;
                    case FrameType.End:
                        if (handle) {
                            handle.onEnd(frame.code ?? 0, frame.message || '');
                            this.streams.delete(frame.streamId);
                        }
                        break;
                    case FrameType.Reset:
                        if (handle) {
                            handle.onReset(frame.message || '');
                            this.streams.delete(frame.streamId);
                        }
                        break;
                    case FrameType.TokenRefresh:
                        // 更新本地 session token
                        if (frame.token) {
                            try { localStorage.setItem('x-session-id', frame.token); } catch {}
                        }
                        break;
                    case FrameType.Pong:
                        // 服务端 pong，忽略即可
                        break;
                    default:
                        break;
                }
            } catch (e) {
                console.error('MuxConnection: decodeFrame 失败', e);
            }
        };

        this.socket.onclose = () => {
            this._onSocketClosed();
        };

        this.socket.onerror = (err) => {
            console.error('MuxConnection: socket 错误', err);
            // onerror 之后通常紧跟 onclose，让 onclose 负责重连
        };
    }

    /** socket 关闭后的清理与重连调度 */
    _onSocketClosed() {
        this.isOpen = false;
        this.socket = null;
        this._stopHeartbeat();

        // 通知所有活跃流关闭
        for (const [, handle] of this.streams) {
            handle.opts.onClose?.();
        }
        this.streams.clear();
        this.pendingOpens = [];

        if (!this.destroyed) {
            this._scheduleReconnect();
        }
    }

    /** 指数退避重连（1s→2s→...→30s 上限） */
    _scheduleReconnect() {
        if (this.destroyed || this.reconnectTimer) return;
        this.reconnectCount++;
        const delay = Math.min(RECONNECT_BASE_MS * Math.pow(2, this.reconnectCount - 1), RECONNECT_MAX_MS);
        this.reconnectTimer = setTimeout(() => {
            this.reconnectTimer = null;
            if (!this.destroyed && !this.socket) {
                this._connect();
            }
        }, delay);
    }

    /** 定期发送 PING 帧（streamId=0）维持连接活跃 */
    _startHeartbeat() {
        this._stopHeartbeat();
        this.heartbeatTimer = setInterval(() => {
            if (this.isOpen && this.socket) {
                const frame = encodeFrame({ type: FrameType.Ping, streamId: 0 });
                this.socket.send(frame.buffer);
            }
        }, HEARTBEAT_INTERVAL_MS);
    }

    _stopHeartbeat() {
        if (this.heartbeatTimer) {
            clearInterval(this.heartbeatTimer);
            this.heartbeatTimer = null;
        }
    }
}

/** 模块级 MuxConnection 单例（懒建，首次 openStream 时创建） */
let _mux = null;

function getMux() {
    if (!_mux) _mux = new MuxConnection();
    return _mux;
}

/**
 * 主动销毁多路复用连接（用于登出等场景）。
 * 调用后：停止重连调度、清除心跳、关闭 socket、向所有活跃流触发 onClose、
 * 清空流表，并将单例重置为 null——下一次 openStream 将重建全新连接。
 *
 * 注意：不会因 token 缺失而自动调用（匿名连接如 web/memory 须正常重连）；
 * 仅此显式调用才会抑制重连。
 */
export function disconnectMux() {
    if (!_mux) return;
    const mux = _mux;
    _mux = null;

    // 标记已销毁，阻止 _scheduleReconnect 内部的 _connect 回调
    mux.destroyed = true;

    // 清除重连定时器
    if (mux.reconnectTimer) {
        clearTimeout(mux.reconnectTimer);
        mux.reconnectTimer = null;
    }

    // 停止心跳
    mux._stopHeartbeat();

    // 关闭 socket（触发 onclose，但 destroyed=true 不会重连）
    if (mux.socket) {
        try { mux.socket.close(); } catch {}
    }

    // 向所有活跃流触发 onClose，并清空流表
    for (const [, handle] of mux.streams) {
        try { handle.opts.onClose?.(); } catch {}
    }
    mux.streams.clear();
    mux.pendingOpens = [];
}

// ==================== Export wrapped service ====================
// reqPaths/respPaths 由生成器按 Input/Output 描述符算出（string[][]）；无 bytes 的方法不传。
const service = {
    post: (url, data, opts, reqPaths, respPaths) => {
        if (reqPaths) applyBytesPaths(data, reqPaths, encodeLeaf);
        const p = getBaseService().post(url, data, opts);
        return respPaths ? p.then((r) => applyBytesPaths(r, respPaths, decodeLeaf)) : p;
    },
    get: (url, data, opts) => {
        const baseService = getBaseService();
        if (baseService.get) {
            return baseService.get(url, data, opts);
        }
        return Promise.reject(new Error("GET method not supported by base service"));
    },
    stream: (url, data, opts, reqPaths, respPaths) => {
        if (reqPaths) applyBytesPaths(data, reqPaths, encodeLeaf);
        const reqStream = activeAdapter.requestStream || defaultRequestStream;
        let finalOpts = opts || {};
        if (respPaths && finalOpts.onChunk) {
            const userOnChunk = finalOpts.onChunk;
            finalOpts = { ...finalOpts, onChunk: (chunk) => userOnChunk(applyBytesPaths(chunk, respPaths, decodeLeaf)) };
        }
        return reqStream(url, data, finalOpts);
    },
    websocket: (url, data, opts, reqPaths, respPaths) =>
        getMux().openStream(url, { ...opts, _initData: data, _reqBytesPaths: reqPaths, _respBytesPaths: respPaths }),
};

export default service;
