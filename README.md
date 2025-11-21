# protoc-gen-frontend-api

一个用于从 Protocol Buffers 定义自动生成前端 API 调用代码的 protoc 插件。

## 功能特性

- 🚀 **自动生成前端 API 代码**：从 `.proto` 文件中提取带有 `google.api.http` 注解的 RPC 方法，自动生成对应的前端 JavaScript/TypeScript API 调用代码
- 📝 **支持多种 HTTP 方法**：支持 POST、GET、PUT、DELETE、PATCH 等 HTTP 方法
- 🎯 **智能命名**：自动将服务名称转换为小驼峰命名（如 `GoodsService` → `goodsApi.js`）
- ⚙️ **可配置**：支持自定义 service 导入路径和输出目录
- 🔧 **自动创建目录**：如果输出目录不存在，插件会自动创建
- 🎁 **多路径输出**：支持一次生成 API 到多个路径，方便多个前端项目共用

## 安装

### 方式一：使用 go install

```bash
go install github.com/lhdbsbz/protoc-gen-frontend-api@latest
```

### 方式二：从源码安装

```bash
git clone https://github.com/lhdbsbz/protoc-gen-frontend-api.git
cd protoc-gen-frontend-api
go install .
```

安装完成后，确保 `$GOPATH/bin` 或 `$GOBIN` 在 `$PATH` 环境变量中。

## 使用方法

### 基本用法（单路径输出）

```bash
protoc \
  --plugin=protoc-gen-frontend-api=$(go env GOPATH)/bin/protoc-gen-frontend-api \
  --frontend-api_out=./src/api \
  --frontend-api_opt=service_import=./api.js \
  proto/goods/goods.proto
```

### 多路径输出（推荐）

如果你有多个前端项目需要共用 API，可以使用 `output_paths` 参数一次生成到多个路径：

```bash
protoc \
  --plugin=protoc-gen-frontend-api=$(go env GOPATH)/bin/protoc-gen-frontend-api \
  --frontend-api_out=./dummy \
  --frontend-api_opt=service_import=@/api/request.js,output_paths=../uni-app/api/grpc-gateway;../another-app/api/grpc-gateway \
  proto/**/*.proto
```

**注意**：使用 `output_paths` 时，`--frontend-api_out` 参数会被忽略，但 protoc 要求必须提供，可以设置为任意值（如 `./dummy`）。

### 高级用法：每个路径使用不同的 service_import

```bash
protoc \
  --plugin=protoc-gen-frontend-api=$(go env GOPATH)/bin/protoc-gen-frontend-api \
  --frontend-api_out=./dummy \
  --frontend-api_opt=service_import=@/api/request.js,output_paths=../uni-app/api/grpc-gateway:@/api/request.js;../another-app/api/grpc-gateway:@/api/api.js \
  proto/**/*.proto
```

格式说明：`path1:import1;path2:import2`，用分号分隔多个路径，用冒号分隔路径和该路径的 service_import。

## 配置选项

插件支持以下配置参数（通过 `--frontend-api_opt` 传递）：

| 参数 | 说明 | 默认值 | 示例 |
|------|------|--------|------|
| `service_import` | service 导入路径（相对路径或别名路径） | `./api.js` | `@/api/api.js` 或 `../api.js` |
| `output_dir` | 输出目录路径（可选，用于向后兼容） | 无 | `./src/api/grpc-gateway` |
| `output_paths` | 多个输出路径（用分号分隔） | 无 | `path1;path2` 或 `path1:import1;path2:import2` |

### 参数格式

多个参数使用逗号分隔：

```bash
--frontend-api_opt=service_import=@/api/api.js,output_paths=path1;path2
```

## 生成的代码格式

### 输入示例（proto 文件）

```protobuf
syntax = "proto3";

import "google/api/annotations.proto";

service UserService {
  // 获取用户信息
  rpc GetUserInfo(GetUserInfoReq) returns (GetUserInfoResp) {
    option (google.api.http) = {
      post: "/grpc-gateway/UserService/GetUserInfo"
      body: "*"
    };
  }
  
  // 更新用户昵称
  rpc UpdateNickName(UpdateNickNameReq) returns (common.Empty) {
    option (google.api.http) = {
      post: "/grpc-gateway/UserService/UpdateNickName"
      body: "*"
    };
  }
  
  // 查询支付状态
  rpc PaymentStatus(PaymentStatusReq) returns (PaymentStatusResp) {
    option (google.api.http) = {
      get: "/grpc-gateway/PaymentService/PaymentStatus"
    };
  }
}
```

### 输出示例（生成的 JavaScript 文件）

```javascript
import service from '@/api/request.js';

export const userApi = {
    GetUserInfo: (data) => service.post('/grpc-gateway/UserService/GetUserInfo', data),
    UpdateNickName: (data) => service.post('/grpc-gateway/UserService/UpdateNickName', data),
    PaymentStatus: (data) => service.get('/grpc-gateway/PaymentService/PaymentStatus', data),
};

export default userApi;
```

## 文件命名规则

- 服务名称会自动去掉 `Service` 后缀
- 文件名使用小驼峰命名（camelCase）
- 示例：
  - `UserService` → `userApi.js`
  - `PostingService` → `postingApi.js`
  - `CourseService` → `courseApi.js`
  - `PaymentService` → `paymentApi.js`

## 在 Makefile 中使用

### 单路径生成

```makefile
FRONTEND_API_DIR := ./src/api/grpc-gateway

build:
	@echo "Generating frontend API files..."
	@mkdir -p $(FRONTEND_API_DIR)
	protoc \
		--plugin=protoc-gen-frontend-api=$(shell go env GOPATH)/bin/protoc-gen-frontend-api \
		--proto_path=. \
		--proto_path=./proto_third \
		--frontend-api_out=$(FRONTEND_API_DIR) \
		--frontend-api_opt=service_import=@/api/request.js,output_dir=$(FRONTEND_API_DIR) \
		proto/**/*.proto
	@echo "Frontend API files generated in $(FRONTEND_API_DIR)"
```

### 多路径生成（推荐）

```makefile
# 多个前端 API 输出路径（用分号分隔）
FRONTEND_API_PATHS := ../uni-app/api/grpc-gateway;../another-app/api/grpc-gateway

build:
	@echo "Generating frontend API files to multiple paths..."
	protoc \
		--plugin=protoc-gen-frontend-api=$(shell go env GOPATH)/bin/protoc-gen-frontend-api \
		--proto_path=. \
		--proto_path=./proto_third \
		--frontend-api_out=./dummy \
		--frontend-api_opt=service_import=@/api/request.js,output_paths=$(FRONTEND_API_PATHS) \
		proto/**/*.proto
	@echo "Frontend API files generated in multiple paths"
```

## 在前端项目中使用

### 导入 API

```javascript
import { userApi } from '@/api/grpc-gateway/userApi.js';
import { postingApi } from '@/api/grpc-gateway/postingApi.js';
```

### 调用 API

```javascript
// POST 请求
const userInfo = await userApi.GetUserInfo({ userId: 123 });

// 更新用户信息
await userApi.UpdateNickName({ nickName: '新昵称' });

// GET 请求
const status = await paymentApi.PaymentStatus({ outTradeNo: '123456' });

// 创建帖子
const result = await postingApi.CreatePosting({
  content: '这是帖子内容',
  images: ['url1', 'url2']
});
```

### 错误处理

```javascript
try {
  const result = await userApi.GetUserInfo({ userId: 123 });
  console.log('成功:', result);
} catch (error) {
  console.error('请求失败:', error.message);
  uni.showToast({
    title: error.message || '请求失败',
    icon: 'none'
  });
}
```

## 注意事项

1. **只生成带 HTTP 注解的方法**：只有带有 `google.api.http` 注解的 RPC 方法才会被生成到前端 API 文件中
2. **需要 google.api.http 依赖**：确保你的 proto 文件导入了 `google/api/annotations.proto`
3. **输出目录**：如果指定了 `output_paths` 参数，插件会自动创建目录（如果不存在）
4. **多路径输出**：如果指定了 `output_paths`，插件会忽略 `--frontend-api_out` 参数，直接使用 `output_paths` 中指定的路径
5. **service 导入路径**：确保前端项目中存在对应的 service 文件（如 `@/api/request.js`），该文件应该导出 `get`、`post`、`put`、`delete`、`patch` 等方法

## service 文件示例

前端项目需要提供一个 service 文件，例如 `@/api/request.js`：

```javascript
// @/api/request.js
const BASE_URL = 'https://api.example.com';

const request = (url, data, method = 'POST') => {
  return new Promise((resolve, reject) => {
    uni.request({
      url: BASE_URL + url,
      method: method,
      data: data,
      header: {
        'Content-Type': 'application/json',
        'x-session-id': getToken() // 从 auth.js 获取 token
      },
      success: (res) => {
        if (res.statusCode === 200 && res.data.code === 0) {
          resolve(res.data.data);
        } else {
          reject(new Error(res.data.message || '请求失败'));
        }
      },
      fail: (err) => {
        reject(new Error('网络请求失败'));
      }
    });
  });
};

export default {
  get: (url, data) => request(url, data, 'GET'),
  post: (url, data) => request(url, data, 'POST'),
  put: (url, data) => request(url, data, 'PUT'),
  delete: (url, data) => request(url, data, 'DELETE'),
  patch: (url, data) => request(url, data, 'PATCH')
};
```

## 开发

### 本地开发

```bash
# 克隆项目
git clone https://github.com/lhdbsbz/protoc-gen-frontend-api.git
cd protoc-gen-frontend-api

# 安装依赖
go mod download

# 安装插件
go install .
```

### 测试

```bash
# 测试生成
protoc \
  --plugin=protoc-gen-frontend-api=$(go env GOPATH)/bin/protoc-gen-frontend-api \
  --frontend-api_out=./test_output \
  --frontend-api_opt=service_import=./api.js \
  test/test.proto
```

## 常见问题

### Q: 为什么生成的代码中没有某个方法？

A: 只有带有 `google.api.http` 注解的 RPC 方法才会被生成。请检查 proto 文件中的方法是否添加了 HTTP 注解。

### Q: 如何支持多个前端项目？

A: 使用 `output_paths` 参数，用分号分隔多个路径即可。

### Q: 生成的代码中 service 导入路径不对怎么办？

A: 可以通过 `service_import` 参数自定义导入路径，或者在 `output_paths` 中为每个路径单独指定。

### Q: 支持 TypeScript 吗？

A: 当前版本生成的是 JavaScript 文件（`.js`），但代码使用 ES6 语法，可以在 TypeScript 项目中直接使用。如果需要生成 `.ts` 文件，可以修改插件源码。

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！

## 相关链接

- [Protocol Buffers](https://developers.google.com/protocol-buffers)
- [gRPC Gateway](https://github.com/grpc-ecosystem/grpc-gateway)
- [protogen](https://pkg.go.dev/google.golang.org/protobuf/compiler/protogen)

