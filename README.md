# protoc-gen-frontend-api

一个用于从 Protocol Buffers 定义自动生成前端 API 调用代码的 protoc 插件。

## 功能特性

- 🚀 **自动生成前端 API 代码**：从 `.proto` 文件中提取带有 `google.api.http` 注解的 RPC 方法，自动生成对应的前端 JavaScript/TypeScript API 调用代码
- 📝 **支持多种 HTTP 方法**：支持 POST、GET、PUT、DELETE、PATCH 等 HTTP 方法
- 🎯 **智能命名**：自动将服务名称转换为小驼峰命名（如 `GoodsService` → `goodsApi.js`）
- ⚙️ **可配置**：支持自定义 service 导入路径和输出目录
- 🔧 **自动创建目录**：如果输出目录不存在，插件会自动创建

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

### 基本用法

```bash
protoc \
  --plugin=protoc-gen-frontend-api=$(go env GOBIN)/protoc-gen-frontend-api \
  --frontend-api_out=./src/api \
  --frontend-api_opt=service_import=./api.js \
  proto/goods/goods.proto
```

### 在 Makefile 中使用

```makefile
FRONTEND_API_DIR := ./src/api

build:
	protoc \
		--plugin=protoc-gen-frontend-api=$(shell go env GOBIN)/protoc-gen-frontend-api \
		--frontend-api_out=$(FRONTEND_API_DIR) \
		--frontend-api_opt=service_import=@/api/api.js,output_dir=$(FRONTEND_API_DIR) \
		proto/**/*.proto
```

## 配置选项

插件支持以下配置参数（通过 `--frontend-api_opt` 传递）：

| 参数 | 说明 | 默认值 | 示例 |
|------|------|--------|------|
| `service_import` | service 导入路径（相对路径或别名路径） | `./api.js` | `@/api/api.js` 或 `../api.js` |
| `output_dir` | 输出目录路径（可选，如果提供则自动创建） | 无 | `./src/api/grpc-gateway` |

### 参数格式

多个参数使用逗号分隔：

```bash
--frontend-api_opt=service_import=@/api/api.js,output_dir=./src/api/grpc-gateway
```

## 生成的代码格式

### 输入示例（proto 文件）

```protobuf
service GoodsService {
  rpc ShopGetProduct(ShopGetProductReq) returns (ShopGetProductResp) {
    option (google.api.http) = {
      post: "/dreame-pt-mall/grpc-gateway/GoodsService/ShopGetProduct"
      body: "*"
    };
  };
  
  rpc ShopListProducts(ShopListProductsReq) returns (ShopListProductsResp) {
    option (google.api.http) = {
      post: "/dreame-pt-mall/grpc-gateway/GoodsService/ShopListProducts"
      body: "*"
    };
  };
}
```

### 输出示例（生成的 JavaScript 文件）

```javascript
import service from '@/api/api.js';

export const goodsApi = {
    ShopGetProduct: (data) => service.post('/dreame-pt-mall/grpc-gateway/GoodsService/ShopGetProduct', data),
    ShopListProducts: (data) => service.post('/dreame-pt-mall/grpc-gateway/GoodsService/ShopListProducts', data),
};

export default goodsApi;
```

## 文件命名规则

- 服务名称会自动去掉 `Service` 后缀
- 文件名使用小驼峰命名（camelCase）
- 示例：
  - `GoodsService` → `goodsApi.js`
  - `ConfigCenterService` → `configCenterApi.js`
  - `UserOrderService` → `userOrderApi.js`

## 注意事项

1. **只生成带 HTTP 注解的方法**：只有带有 `google.api.http` 注解的 RPC 方法才会被生成到前端 API 文件中
2. **需要 google.api.http 依赖**：确保你的 proto 文件导入了 `google/api/annotations.proto`
3. **输出目录**：如果指定了 `output_dir` 参数，插件会自动创建目录（如果不存在）
4. **服务注释**：插件不会生成硬编码的服务注释，如果 proto 文件中有注释，可以扩展插件来读取

## 示例项目

### 完整的 Makefile 示例

```makefile
FRONTEND_API_DIR := ./src/api/grpc-gateway

build:
	@echo "Generating frontend API files..."
	@mkdir -p $(FRONTEND_API_DIR)
	protoc \
		--plugin=protoc-gen-frontend-api=$(shell go env GOBIN)/protoc-gen-frontend-api \
		--proto_path=. \
		--proto_path=./third_party \
		--frontend-api_out=$(FRONTEND_API_DIR) \
		--frontend-api_opt=service_import=@/api/api.js,output_dir=$(FRONTEND_API_DIR) \
		proto/**/*.proto
	@echo "Frontend API files generated in $(FRONTEND_API_DIR)"
```

### 在前端项目中使用

```javascript
import { goodsApi } from '@/api/grpc-gateway/goodsApi.js';

// 调用 API
const product = await goodsApi.ShopGetProduct({ id: 123 });
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
  --plugin=protoc-gen-frontend-api=$(go env GOBIN)/protoc-gen-frontend-api \
  --frontend-api_out=./test_output \
  --frontend-api_opt=service_import=./api.js \
  test/test.proto
```

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！

## 相关链接

- [Protocol Buffers](https://developers.google.com/protocol-buffers)
- [gRPC Gateway](https://github.com/grpc-ecosystem/grpc-gateway)
- [protogen](https://pkg.go.dev/google.golang.org/protobuf/compiler/protogen)

