# protoc-gen-frontend-api

从带 `google.api.http` 的 proto 生成前端 API 封装（`*Api.ts` 或 `*Api.js`）。**不生成类型**：TS 版引用 [ts-proto](https://github.com/stephenh/ts-proto) 的类型；JS 版无类型、不依赖 proto-types。

---

## 安装

```bash
go install github.com/lhdbsbz/protoc-gen-frontend-api@main
```

确保 `$(go env GOBIN)` 或 `$GOPATH/bin` 在 `PATH` 中。

---

## `--frontend-api_opt` 参数

逗号分隔的 `key=value`。键名采用与常见 protoc 插件一致的 **snake_case**（如 `paths=source_relative`）。**未知键会直接报错**。

| 键 | 含义 | 默认 |
|----|------|------|
| `typescript_outputs` | TS 输出目录，多个用 `;`；每项可为 `dir` 或 `dir:该目录专用 service 路径` | — |
| `javascript_outputs` | JS 输出目录，格式同上 | — |
| `types_from` | ts-proto 类型根路径（仅 TS 生成需要） | `@/api/proto-types` |
| `service_import` | `import service from '…'` 的路径（TS；JS 未单独指定时沿用） | `./api` |
| `service_import_js` | 仅 JS 输出的 service 路径 | 同 `service_import` |

`typescript_outputs` 与 `javascript_outputs` 至少配置其一才有输出。生成前会**清空**所列目录。

`--frontend-api_out` 是 protoc 要求的输出根路径，建议与对应列表中的**第一个目录**一致；实际写文件仍按 opt 里的路径。

---

## TypeScript 示例

1. 先用 **ts-proto** 生成类型（如 `onlyTypes=true`）。
2. 调用本插件：

```bash
protoc --proto_path=. --proto_path=proto_third \
  --plugin=protoc-gen-frontend-api=$(go env GOBIN)/protoc-gen-frontend-api \
  --frontend-api_out=src/api/grpc-gateway \
  --frontend-api_opt=typescript_outputs=src/api/grpc-gateway,types_from=@/api/proto-types,service_import=@/api/api \
  proto/**/*.proto
```

---

## JavaScript 示例

无需 ts-proto：

```bash
protoc --proto_path=. --proto_path=proto_third \
  --plugin=protoc-gen-frontend-api=$(go env GOBIN)/protoc-gen-frontend-api \
  --frontend-api_out=src/api/grpc-gateway \
  --frontend-api_opt=javascript_outputs=src/api/grpc-gateway,service_import_js=@/api/api.js \
  proto/**/*.proto
```

---

## 生成物示例

**TS（`userApi.ts`）**

```ts
import service from '@/api/api';
import type { GetUserReq, GetUserResp } from '@/api/proto-types/proto/user/user';

export const userApi = {
  GetUser: (data: GetUserReq): Promise<GetUserResp> => service.post('/xxx/UserService/GetUser', data),
};
export default userApi;
```

**JS（`userApi.js`）**

```js
import service from '@/api/api.js';

export const userApi = {
  GetUser: (data) => service.post('/xxx/UserService/GetUser', data),
};
export default userApi;
```

`UserService` → 文件名 `userApi`。

---

## 对 service 模块的要求

`service_import` / `service_import_js` 指向的模块需 **默认导出** 含 `get`、`post`、`put`、`delete`、`patch` 的对象（如 axios 实例）。

---

## 多目录 / TS+JS 同时出

```bash
--frontend-api_opt=typescript_outputs=app1/src/api/grpc-gateway;app2/src/api/grpc-gateway,types_from=@/api/proto-types,service_import=@/api/api

--frontend-api_opt=typescript_outputs=frontend-ts/src/api/grpc-gateway,javascript_outputs=frontend-js/src/api/grpc-gateway,service_import=@/api/api,service_import_js=@/api/api.js,types_from=@/api/proto-types
```

---

## 常见问题

- **RPC 没进 API？** 只处理带 `option (google.api.http) = { … }` 的方法。
- **TS 找不到 proto-types？** 先跑 ts-proto；核对 `types_from` 与 ts-proto 输出、路径别名一致。
- **还要不要 Makefile 里 `rm -rf` 前端目录？** 不必，插件会清空 `typescript_outputs` / `javascript_outputs` 所列目录。

---

## 开发

```bash
git clone https://github.com/lhdbsbz/protoc-gen-frontend-api.git && cd protoc-gen-frontend-api
go build && go install .
```

MIT · [ts-proto](https://github.com/stephenh/ts-proto) · [gRPC-Gateway](https://github.com/grpc-ecosystem/grpc-gateway)
