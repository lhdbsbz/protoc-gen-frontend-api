# protoc-gen-frontend-api

从带 `google.api.http` 的 proto 生成前端 API 封装（`*Api.ts` 或 `*Api.js`）。**不生成类型**，TS 版引用 [ts-proto](https://github.com/stephenh/ts-proto) 的类型；JS 版无类型、不依赖 proto-types。

---

## 安装

```bash
go install github.com/lhdbsbz/protoc-gen-frontend-api@main
```

确保 `$(go env GOBIN)` 或 `$GOPATH/bin` 在 `PATH` 中。

---

## 快速开始

### TypeScript 项目

1. 先用 **ts-proto** 生成类型到 `proto-types/`（`onlyTypes=true`）。
2. 在 protoc 中挂上本插件，传入 `output_paths` 和 `types_import_path`：

```bash
protoc --proto_path=. --proto_path=proto_third \
  --plugin=protoc-gen-frontend-api=$(go env GOBIN)/protoc-gen-frontend-api \
  --frontend-api_out=. \
  --frontend-api_opt=service_import=@/api/api,output_paths="src/api/grpc-gateway",types_import_path=@/api/proto-types \
  proto/**/*.proto
```

生成 `userApi.ts`、`orderApi.ts` 等，`import type` 来自 proto-types。

### JavaScript 项目

只传 `output_paths_js`，**无需** proto-types、无需 ts-proto：

```bash
protoc --proto_path=. --proto_path=proto_third \
  --plugin=protoc-gen-frontend-api=$(go env GOBIN)/protoc-gen-frontend-api \
  --frontend-api_out=. \
  --frontend-api_opt=service_import_js=@/api/api.js,output_paths_js="src/api/grpc-gateway" \
  proto/**/*.proto
```

生成 `userApi.js`、`orderApi.js` 等，无类型 import，`(data) => service.post('path', data)` 风格。

---

## 参数

`--frontend-api_opt=` 内用逗号分隔，格式：`key=value`。

| 参数 | 含义 | 默认 |
|------|------|------|
| `output_paths` | TS 输出目录，多个用 `;` | — |
| `output_paths_js` | JS 输出目录，多个用 `;` | — |
| `service_import` | TS 的 service 导入（如 `@/api/api`） | `./api` |
| `service_import_js` | JS 的 service 导入（如 `@/api/api.js`） | 同 `service_import` |
| `types_import_path` | ts-proto 类型根路径（仅 TS） | `@/api/proto-types` |

**路径格式**：`path1;path2` 或 `path1:自定义service导入;path2`。

**说明**：`--frontend-api_out` 为 protoc 必填，本插件不读，填 `.` 即可；实际输出由 `output_paths` / `output_paths_js` 决定。生成前会清空这些目录，Makefile 不必再 `rm -rf`。

---

## 生成示例

**Proto**：RPC 带 `option (google.api.http) = { post: "/xxx" body: "*" }`。

**TS（userApi.ts）**：

```ts
import service from '@/api/api';
import type { GetUserReq, GetUserResp } from '@/api/proto-types/proto/user/user';

export const userApi = {
  GetUser: (data: GetUserReq): Promise<GetUserResp> => service.post('/xxx/UserService/GetUser', data),
};
export default userApi;
```

**JS（userApi.js）**：

```js
import service from '@/api/api.js';

export const userApi = {
  GetUser: (data) => service.post('/xxx/UserService/GetUser', data),
};
export default userApi;
```

服务名去 `Service`、首字母小写即文件名：`UserService` → `userApi`。

---

## 对 service 的要求

`service_import` 指向的模块需 **默认导出** 含 `get`、`post`、`put`、`delete`、`patch` 的对象，例如基于 axios 的封装：

```ts
const service = axios.create({ baseURL: '...' });
export default service;
```

---

## 常用写法

```bash
# 多项目 TS，共用一个 types
--frontend-api_opt=output_paths="proj1/src/api/grpc-gateway;proj2/src/api/grpc-gateway",types_import_path=@/api/proto-types,service_import=@/api/api

# TS + JS 同时出
--frontend-api_opt=output_paths="mulan/src/api/grpc-gateway",output_paths_js="shop-operating/src/api/grpc-gateway",service_import=@/api/api,service_import_js=@/api/api.js,types_import_path=@/api/proto-types
```

---

## 常见问题

- **RPC 没出现在 API 里？** 只处理带 `google.api.http` 的 RPC，检查是否加了 `option (google.api.http) = { ... }`。
- **TS 报 `Cannot find module '@/api/proto-types/...'`？** 先跑 ts-proto；确认 `types_import_path`、ts-proto 的 `--ts_proto_out` 与项目路径/别名一致。
- **Makefile 要 `rm -rf` 前端 API 目录吗？** 不要，插件会在生成前清空 `output_paths` / `output_paths_js`。
- **JS 要跑 ts-proto 吗？** 不要，`output_paths_js` 不依赖 proto-types。

---

## 开发

```bash
git clone https://github.com/lhdbsbz/protoc-gen-frontend-api.git && cd protoc-gen-frontend-api
go build && go install .
```

MIT · [ts-proto](https://github.com/stephenh/ts-proto) · [gRPC-Gateway](https://github.com/grpc-ecosystem/grpc-gateway)
