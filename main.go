package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// 输出路径配置
type OutputPathConfig struct {
	Path          string // 输出路径
	ServiceImport string // 该路径对应的 service 导入路径（可选，如果为空则使用全局的）
}

// 插件配置
type PluginConfig struct {
	ServiceImport   string             // service 导入路径（TS，及 JS 在未指定 service_import_js 时）
	ServiceImportJS string             // JS 专用 service 导入路径（可选，如 '@/api/api.js'）
	TypesImportPath string             // 类型定义导入路径前缀（如 '@/api/proto-types'，仅 TS 使用）
	OutputPaths     []OutputPathConfig // TS 输出路径
	OutputPathsJS   []OutputPathConfig // JS 输出路径（按 addressApi.js 风格，无类型 import）
}

// 方法信息结构体
type MethodInfo struct {
	MethodName   string // 方法名称
	HttpPath     string // HTTP 路径
	HttpMethod   string // HTTP 方法（post, get等）
	RequestType  string // 请求类型名称（用于 TS）
	ResponseType string // 响应类型名称（用于 TS）
}

// 服务信息结构体
type ServiceInfo struct {
	ServiceName     string              // 服务名称（去掉 Service 后缀）
	ApiFileName     string              // API 文件名（如 productApi）
	Methods         []MethodInfo        // 方法列表
	ServiceImport   string              // service 导入路径
	TypesImportPath string              // 类型定义导入路径前缀（如 @/api/proto-types）
	TypeImports     map[string][]string // 需要导入的类型列表 (importPath -> sortedTypeNames)
}

func main() {
	protogen.Options{}.Run(func(gen *protogen.Plugin) error {
		// 解析插件参数
		var param string
		if gen.Request.Parameter != nil {
			param = *gen.Request.Parameter
		}

		config, err := parsePluginOptions(param)
		if err != nil {
			return fmt.Errorf("解析插件参数失败: %v", err)
		}

		// 生成前清空各输出目录，确保只保留本次生成的文件（便于 proto 删除服务时移除旧 API）
		for _, outputPath := range config.OutputPaths {
			if err := clearOutputDir(outputPath.Path); err != nil {
				return fmt.Errorf("清空输出目录失败 %s: %v", outputPath.Path, err)
			}
		}
		for _, outputPath := range config.OutputPathsJS {
			if err := clearOutputDir(outputPath.Path); err != nil {
				return fmt.Errorf("清空输出目录失败(JS) %s: %v", outputPath.Path, err)
			}
		}

		for _, f := range gen.Files {
			if !f.Generate {
				continue
			}

			// 查找服务定义
			for _, service := range f.Services {
				// 生成前端 API 文件
				if err := generateFrontendApi(gen, f, service, config); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// parsePluginOptions 解析插件参数
func parsePluginOptions(param string) (*PluginConfig, error) {
	config := &PluginConfig{
		ServiceImport:   "./api",             // 默认 service 导入路径
		ServiceImportJS: "",                  // 为空时 JS 使用 ServiceImport
		TypesImportPath: "@/api/proto-types", // 默认类型定义导入路径
		OutputPaths:     []OutputPathConfig{},
		OutputPathsJS:   []OutputPathConfig{},
	}

	if param == "" {
		return config, nil
	}

	// 解析参数，格式: key1=value1,key2=value2
	pairs := strings.Split(param, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])

		switch key {
		case "service_import":
			config.ServiceImport = value
		case "service_import_js":
			config.ServiceImportJS = value
		case "types_import_path":
			config.TypesImportPath = value
		case "output_paths":
			// 解析输出路径，格式: path1;path2;path3 或 path1:import1;path2:import2
			config.OutputPaths = parseOutputPaths(value)
		case "output_paths_js":
			config.OutputPathsJS = parseOutputPaths(value)
		}
	}

	return config, nil
}

// clearOutputDir 清空输出目录：删除目录内所有内容后重建该目录
// 若目录不存在，则什么也不做、不报错
func clearOutputDir(dir string) error {
	if dir == "" {
		return nil
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.MkdirAll(dir, 0755)
}

// parseOutputPaths 解析输出路径配置
func parseOutputPaths(value string) []OutputPathConfig {
	var paths []OutputPathConfig
	for _, pathStr := range strings.Split(value, ";") {
		pathStr = strings.TrimSpace(pathStr)
		if pathStr == "" {
			continue
		}
		// 检查是否包含 service_import，格式: path:import
		if parts := strings.SplitN(pathStr, ":", 2); len(parts) == 2 {
			paths = append(paths, OutputPathConfig{
				Path:          strings.TrimSpace(parts[0]),
				ServiceImport: strings.TrimSpace(parts[1]),
			})
		} else {
			// 只有路径，使用全局的 service_import
			paths = append(paths, OutputPathConfig{
				Path:          pathStr,
				ServiceImport: "", // 空字符串表示使用全局的
			})
		}
	}
	return paths
}

// generateFrontendApi 生成前端 API 文件
func generateFrontendApi(gen *protogen.Plugin, file *protogen.File, service *protogen.Service, config *PluginConfig) error {
	// 服务名称（去掉 Service 后缀）
	serviceName := strings.TrimSuffix(string(service.Desc.Name()), "Service")

	// 生成 API 文件名（例如：GoodsService -> goodsApi）
	apiFileName := toCamelCase(serviceName) + "Api"

	// 提取方法信息
	var methods []MethodInfo
	for _, method := range service.Methods {
		// 只处理有 HTTP 注解的方法
		if httpRule := extractHttpRule(method); httpRule != nil {
			// 获取请求和响应类型名称
			requestType := string(method.Input.Desc.Name())
			responseType := string(method.Output.Desc.Name())

			methodInfo := MethodInfo{
				MethodName:   string(method.Desc.Name()),
				HttpPath:     httpRule.Path,
				HttpMethod:   strings.ToLower(httpRule.Method),
				RequestType:  requestType,
				ResponseType: responseType,
			}
			methods = append(methods, methodInfo)
		}
	}

	// 如果没有方法，跳过生成
	if len(methods) == 0 {
		return nil
	}

	// 收集所有使用的类型及其所在的 proto 文件
	// 用于生成正确的 import 语句
	typeImports := collectTypeImports(gen, service, methods)

	// 收集所有输出路径配置（只使用 TS 路径）
	allOutputPaths := config.OutputPaths

	// 如果没有配置输出路径，跳过生成
	if len(allOutputPaths) == 0 {
		return nil
	}

	// 对每个路径都生成文件
	for _, outputPathConfig := range allOutputPaths {
		// 确定该路径使用的 service_import
		serviceImport := outputPathConfig.ServiceImport
		if serviceImport == "" {
			serviceImport = config.ServiceImport
		}

		// 准备模板数据
		data := ServiceInfo{
			ServiceName:     serviceName,
			ApiFileName:     apiFileName,
			Methods:         methods,
			ServiceImport:   serviceImport,
			TypesImportPath: config.TypesImportPath,
			TypeImports:     typeImports,
		}

		// 生成 TypeScript 代码
		code := generateTypeScriptCode(data)
		fileName := toCamelCase(serviceName) + "Api.ts"

		// 若输出目录不存在，跳过该路径，不报错
		if _, err := os.Stat(outputPathConfig.Path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("检查输出目录失败 %s: %v", outputPathConfig.Path, err)
		}

		fullPath := filepath.Join(outputPathConfig.Path, fileName)
		if err := os.WriteFile(fullPath, code, 0644); err != nil {
			return fmt.Errorf("写入文件失败 %s: %v", fullPath, err)
		}
	}

	// 按 output_paths_js 生成 JS 接口（无类型 import，(data) => service.{method}('path', data)）
	for _, outputPathConfig := range config.OutputPathsJS {
		serviceImport := outputPathConfig.ServiceImport
		if serviceImport == "" {
			serviceImport = config.ServiceImportJS
		}
		if serviceImport == "" {
			serviceImport = config.ServiceImport
		}
		data := ServiceInfo{
			ServiceName:   serviceName,
			ApiFileName:   apiFileName,
			Methods:       methods,
			ServiceImport: serviceImport,
		}
		code := generateJavaScriptCode(data)
		fileName := toCamelCase(serviceName) + "Api.js"
		if _, err := os.Stat(outputPathConfig.Path); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return fmt.Errorf("检查输出目录失败(JS) %s: %v", outputPathConfig.Path, err)
		}
		fullPath := filepath.Join(outputPathConfig.Path, fileName)
		if err := os.WriteFile(fullPath, code, 0644); err != nil {
			return fmt.Errorf("写入文件失败(JS) %s: %v", fullPath, err)
		}
	}

	return nil
}

// extractHttpRule 从方法中提取 HTTP 规则
func extractHttpRule(method *protogen.Method) *HttpRule {
	// 获取方法的选项
	options, ok := method.Desc.Options().(*descriptorpb.MethodOptions)
	if !ok || options == nil {
		return nil
	}

	// 获取 HTTP 注解
	httpRuleExt := proto.GetExtension(options, annotations.E_Http)
	if httpRuleExt == nil {
		return nil
	}

	rule, ok := httpRuleExt.(*annotations.HttpRule)
	if !ok || rule == nil {
		return nil
	}

	// 使用反射安全地访问 Pattern 字段
	ruleValue := reflect.ValueOf(rule).Elem()
	patternField := ruleValue.FieldByName("Pattern")
	if !patternField.IsValid() || patternField.IsNil() {
		return nil
	}

	// 优先使用 post/get/put/delete/patch 中的路径
	// 使用类型断言访问不同的 HTTP 方法
	patternInterface := patternField.Interface()

	switch v := patternInterface.(type) {
	case *annotations.HttpRule_Post:
		if len(v.Post) > 0 {
			return &HttpRule{
				Method: "post",
				Path:   v.Post,
			}
		}
	case *annotations.HttpRule_Get:
		if len(v.Get) > 0 {
			return &HttpRule{
				Method: "get",
				Path:   v.Get,
			}
		}
	case *annotations.HttpRule_Put:
		if len(v.Put) > 0 {
			return &HttpRule{
				Method: "put",
				Path:   v.Put,
			}
		}
	case *annotations.HttpRule_Delete:
		if len(v.Delete) > 0 {
			return &HttpRule{
				Method: "delete",
				Path:   v.Delete,
			}
		}
	case *annotations.HttpRule_Patch:
		if len(v.Patch) > 0 {
			return &HttpRule{
				Method: "patch",
				Path:   v.Patch,
			}
		}
	}

	return nil
}

// HttpRule HTTP 规则结构
type HttpRule struct {
	Method string
	Path   string
}

// toCamelCase 将首字母转为小写（例如：Goods -> goods）
func toCamelCase(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// uniqueAndSort 去重并排序字符串切片
func uniqueAndSort(strs []string) []string {
	// 去重
	typeSet := make(map[string]bool)
	for _, s := range strs {
		typeSet[s] = true
	}
	var unique []string
	for s := range typeSet {
		unique = append(unique, s)
	}
	// 排序
	sort.Strings(unique)
	return unique
}

// collectTypeImports 收集所有需要的类型导入信息
// 只收集请求和响应类型本身，不递归收集嵌套类型（因为 TypeScript 类型系统会自动处理）
// 返回 map[importPath][]sortedTypeNames，避免重复分组
// methods 参数用于匹配哪些方法需要处理（避免重复调用 extractHttpRule）
func collectTypeImports(gen *protogen.Plugin, service *protogen.Service, methods []MethodInfo) map[string][]string {
	// 创建方法名到 MethodInfo 的映射，用于快速查找
	methodMap := make(map[string]bool)
	for _, m := range methods {
		methodMap[m.MethodName] = true
	}

	typeFileMap := make(map[string]string) // typeName -> protoFilePath

	// 从实际的 method 对象中收集请求和响应类型（使用已提取的 methods 避免重复调用 extractHttpRule）
	for _, method := range service.Methods {
		// 只处理在 methods 列表中的方法（这些已经通过 extractHttpRule 验证）
		if !methodMap[string(method.Desc.Name())] {
			continue
		}

		// 收集请求类型
		if method.Input != nil {
			typeName := string(method.Input.Desc.Name())
			// 使用 Desc.ParentFile() 直接获取文件，O(1) 复杂度
			if fileDesc := method.Input.Desc.ParentFile(); fileDesc != nil {
				typeFileMap[typeName] = fileDesc.Path()
			}
		}

		// 收集响应类型
		if method.Output != nil {
			typeName := string(method.Output.Desc.Name())
			// 使用 Desc.ParentFile() 直接获取文件，O(1) 复杂度
			if fileDesc := method.Output.Desc.ParentFile(); fileDesc != nil {
				typeFileMap[typeName] = fileDesc.Path()
			}
		}
	}

	// 按导入路径分组类型
	importMap := make(map[string][]string) // importPath -> []typeNames
	for typeName, protoFile := range typeFileMap {
		importPath := protoFileToImportPath(protoFile)
		if importPath != "" {
			importMap[importPath] = append(importMap[importPath], typeName)
		}
	}

	// 对每个导入路径的类型列表去重并排序
	for importPath, typeNames := range importMap {
		importMap[importPath] = uniqueAndSort(typeNames)
	}

	return importMap
}

// protoFileToImportPath 将 proto 文件路径转换为 TypeScript 导入路径
// ts-proto 会保留 proto/ 和 proto_third/ 前缀
// 例如: proto/config_center/config_center.proto -> proto/config_center/config_center
// 例如: proto_third/google/protobuf/struct.proto -> proto_third/google/protobuf/struct
func protoFileToImportPath(protoFilePath string) string {
	if protoFilePath == "" {
		return ""
	}

	// 移除 .proto 扩展名
	path := strings.TrimSuffix(protoFilePath, ".proto")

	// 移除开头的 ./（如果存在）
	path = strings.TrimPrefix(path, "./")

	// ts-proto 会保留 proto/ 和 proto_third/ 前缀，所以不需要去掉
	// 只需要确保路径分隔符统一为 /
	path = strings.ReplaceAll(path, "\\", "/")

	return path
}

// generateTypeScriptCode 生成 TypeScript API 代码内容
// 最佳实践：引用 ts-proto 生成的类型定义，而不是自己生成
func generateTypeScriptCode(data ServiceInfo) []byte {
	var buf bytes.Buffer

	// 写入 service import
	serviceImport := data.ServiceImport
	buf.WriteString("import service from '")
	buf.WriteString(serviceImport)
	buf.WriteString("';\n")

	// 写入类型定义导入（从 ts-proto 生成的文件导入），按 importPath 排序以保证生成稳定
	if len(data.TypeImports) > 0 {
		importPaths := make([]string, 0, len(data.TypeImports))
		for k := range data.TypeImports {
			importPaths = append(importPaths, k)
		}
		sort.Strings(importPaths)
		for _, importPath := range importPaths {
			typeNames := data.TypeImports[importPath]
			fullImportPath := data.TypesImportPath
			if !strings.HasSuffix(fullImportPath, "/") && importPath != "" {
				fullImportPath += "/"
			}
			fullImportPath += importPath

			buf.WriteString("import type { ")
			buf.WriteString(strings.Join(typeNames, ", "))
			buf.WriteString(" } from '")
			buf.WriteString(fullImportPath)
			buf.WriteString("';\n")
		}
	}

	buf.WriteString("\n")

	// 生成 API 对象
	buf.WriteString("export const ")
	buf.WriteString(data.ApiFileName)
	buf.WriteString(" = {\n")

	// 写入方法
	for i, method := range data.Methods {
		buf.WriteString("  ")
		buf.WriteString(method.MethodName)
		buf.WriteString(": (data: ")
		buf.WriteString(method.RequestType)
		buf.WriteString("): Promise<")
		buf.WriteString(method.ResponseType)
		buf.WriteString("> =>\n")
		buf.WriteString("    service.")
		buf.WriteString(method.HttpMethod)
		buf.WriteString("('")
		buf.WriteString(method.HttpPath)
		buf.WriteString("', data)")

		if i < len(data.Methods)-1 {
			buf.WriteString(",\n")
		} else {
			buf.WriteString("\n")
		}
	}

	buf.WriteString("};\n\n")
	buf.WriteString("export default ")
	buf.WriteString(data.ApiFileName)
	buf.WriteString(";\n")

	return buf.Bytes()
}

// generateJavaScriptCode 按 addressApi.js 风格生成 JS：无类型 import，(data) => service.{method}('path', data)
func generateJavaScriptCode(data ServiceInfo) []byte {
	var buf bytes.Buffer
	buf.WriteString("import service from '")
	buf.WriteString(data.ServiceImport)
	buf.WriteString("';\n\n")
	buf.WriteString("export const ")
	buf.WriteString(data.ApiFileName)
	buf.WriteString(" = {\n")
	for i, method := range data.Methods {
		buf.WriteString("    ")
		buf.WriteString(method.MethodName)
		buf.WriteString(": (data) => service.")
		buf.WriteString(method.HttpMethod)
		buf.WriteString("('")
		buf.WriteString(method.HttpPath)
		buf.WriteString("', data)")
		if i < len(data.Methods)-1 {
			buf.WriteString(",\n")
		} else {
			buf.WriteString("\n")
		}
	}
	buf.WriteString("};\n\n")
	buf.WriteString("export default ")
	buf.WriteString(data.ApiFileName)
	buf.WriteString(";\n")
	return buf.Bytes()
}
