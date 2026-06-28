import * as baseServiceModule from 'BASE_SERVICE_IMPORT_PLACEHOLDER';
// 循环依赖：request.js 反过来也 import 本模块（default），所以这里不能在模块求值阶段读取它的 default
// （此时 request.js 的 export default 可能尚未执行 → TDZ "Cannot access 'default' before initialization"）。
// 改为惰性读取：下面所有用到 baseService 的地方都在函数运行时才调用，那时 request.js 已初始化完毕。
const getBaseService = () => baseServiceModule.default || baseServiceModule;

// ==================== UTF-8 Polyfill ====================
function stringToUint8Array(str) {
    const arr = [];
    for (let i = 0; i < str.length; i++) {
        const code = str.charCodeAt(i);
        if (code < 0x80) {
            arr.push(code);
        } else if (code < 0x800) {
            arr.push(0xc0 | (code >> 6), 0x80 | (code & 0x3f));
        } else if (code < 0xd800 || code >= 0xe000) {
            arr.push(0xe0 | (code >> 12), 0x80 | ((code >> 6) & 0x3f), 0x80 | (code & 0x3f));
        } else {
            i++;
            const nextCode = str.charCodeAt(i);
            const utf32 = 0x10000 + (((code & 0x3ff) << 10) | (nextCode & 0x3ff));
            arr.push(
                0xf0 | (utf32 >> 18),
                0x80 | ((utf32 >> 12) & 0x3f),
                0x80 | ((utf32 >> 6) & 0x3f),
                0x80 | (utf32 & 0x3f)
            );
        }
    }
    return new Uint8Array(arr);
}

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

class GrpcWebWebSocketClient {
    constructor(url, opts = {}) {
        this.opts = opts;
        this.isOpened = false;
        this.sendQueue = [];
        this._reqBytesPaths = opts._reqBytesPaths;   // 出站帧 bytes 路径（send 时编码）
        this._respBytesPaths = opts._respBytesPaths; // 入站帧 bytes 路径（onMessage 时解码）

        const key1 = 'BASE_URL';
        const key2 = 'baseUrl';
        const baseService = getBaseService();
        const baseUrl = baseServiceModule[key1] || baseServiceModule[key2] || baseService[key2] || baseService[key1] || "";
        const finalUrl = url.startsWith('http') ? url : (baseUrl ? (baseUrl.endsWith('/') ? baseUrl.slice(0, -1) : baseUrl) + url : url);
        let wsUrl = finalUrl.replace(/^http/, 'ws');
        if (wsUrl.startsWith('/')) {
            if (typeof window !== 'undefined' && window.location) {
                const origin = window.location.origin.replace(/^http/, 'ws');
                wsUrl = origin + wsUrl;
            }
        }

        // session token 走 WS 子协议（Sec-WebSocket-Protocol），不进 URL —— 浏览器 WebSocket API
        // 无法设置自定义请求头，子协议是唯一不污染 URL 的鉴权通道，token 因此不再落入网关/反代
        // access log 与浏览器历史。后端 AppUser 中间件从该头解析 'lmcl.bearer.<token>'；升级握手时
        // gorilla 仍回选 'grpc-websockets'，token 子协议仅承载鉴权、不参与协议协商。
        // (JWT 字符集为 base64url + '.'，本身即合法 HTTP token，无需再编码。)
        const subprotocols = ['grpc-websockets'];
        const token = getSessionToken();
        if (token) {
            subprotocols.push('lmcl.bearer.' + token);
        }

        const creator = activeAdapter.createWebSocket || ((u, p) => new BrowserWebSocketWrapper(u, p));
        try {
            this.socket = creator(wsUrl, subprotocols);
        } catch (err) {
            setTimeout(() => opts.onError?.(err), 0);
            return;
        }

        this.socket.onopen = () => {
            this.isOpened = true;
            for (const msg of this.sendQueue) {
                this.socket.send(msg);
            }
            this.sendQueue = [];
            opts.onOpen?.();
        };

        this.socket.onmessage = (event) => {
            const view = new Uint8Array(event.data);
            if (view.length < 5) return;

            const flags = view[0];
            const length = (view[1] << 24) | (view[2] << 16) | (view[3] << 8) | view[4];

            if (flags === 0x00) {
                const payloadData = view.slice(5, 5 + length);
                const jsonStr = uint8ArrayToString(payloadData);
                try {
                    const parsed = JSON.parse(jsonStr);
                    if (this._respBytesPaths) applyBytesPaths(parsed, this._respBytesPaths, decodeLeaf);
                    opts.onMessage?.(parsed);
                } catch (e) {
                    console.error("Failed to parse WebSocket JSON payload:", jsonStr, e);
                }
            } else if (flags === 0x80) {
                const trailerStr = uint8ArrayToString(view.slice(5, 5 + length));
                this._parseTrailer(trailerStr);
                this.close();
            }
        };

        this.socket.onclose = () => {
            opts.onClose?.();
        };

        this.socket.onerror = (err) => {
            opts.onError?.(err instanceof Error ? err : new Error(String(err)));
        };
    }

    send(data) {
        if (this._reqBytesPaths) applyBytesPaths(data, this._reqBytesPaths, encodeLeaf);
        const jsonStr = JSON.stringify(data);
        const pbBytes = stringToUint8Array(jsonStr);
        const buf = new ArrayBuffer(5 + pbBytes.length);
        const view = new Uint8Array(buf);
        view[0] = 0x00;
        view[1] = (pbBytes.length >> 24) & 0xFF;
        view[2] = (pbBytes.length >> 16) & 0xFF;
        view[3] = (pbBytes.length >> 8) & 0xFF;
        view[4] = pbBytes.length & 0xFF;
        view.set(pbBytes, 5);

        if (this.isOpened && this.socket) {
            this.socket.send(buf);
        } else {
            this.sendQueue.push(buf);
        }
    }

    halfClose() {
        const buf = new ArrayBuffer(5);
        const view = new Uint8Array(buf);
        view[0] = 0x80;
        view[1] = 0; view[2] = 0; view[3] = 0; view[4] = 0;

        if (this.isOpened && this.socket) {
            this.socket.send(buf);
        } else {
            this.sendQueue.push(buf);
        }
    }

    close() {
        this.socket?.close();
    }

    _parseTrailer(str) {
        let code = 0;
        let message = "OK";
        const lines = str.split('\r\n');
        for (const line of lines) {
            const parts = line.split(': ');
            if (parts[0] === 'grpc-status') {
                code = parseInt(parts[1], 10);
            } else if (parts[0] === 'grpc-message') {
                message = parts[1];
            }
        }
        if (code !== 0) {
            this.opts.onError?.(new Error(message));
        } else {
            this.opts.onSuccess?.();
        }
    }
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
    websocket: (url, data, opts, reqPaths, respPaths) => {
        return new GrpcWebWebSocketClient(url, { ...opts, ...data, _reqBytesPaths: reqPaths, _respBytesPaths: respPaths });
    }
};

export default service;
