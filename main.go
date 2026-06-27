package main

import (
	"bytes"
	"encoding/json"
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

// 插件配置（对应 --frontend-api_opt）
type PluginConfig struct {
	ServiceImport   string             // TS 默认；JS 在未设 ServiceImportJS 时回退到此
	ServiceImportJS string             // JS 专用 service 导入（可选）
	TypesRoot       string             // ts-proto 类型根路径前缀（仅 TS）
	TsOut           []OutputPathConfig // TypeScript *Api.ts 输出目录
	JsOut           []OutputPathConfig // JavaScript *Api.js 输出目录
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
	ServiceImport string              // service 模块路径
	TypesRoot     string              // ts-proto 类型根路径前缀
	TypeImports   map[string][]string // importPath -> sorted type names
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
			return fmt.Errorf("frontend-api: %w", err)
		}

		// 生成前清空各输出目录，确保只保留本次生成的文件（便于 proto 删除服务时移除旧 API）
		for _, outputPath := range config.TsOut {
			if err := clearOutputDir(outputPath.Path); err != nil {
				return fmt.Errorf("清空 TS 输出目录失败 %s: %v", outputPath.Path, err)
			}
		}
		for _, outputPath := range config.JsOut {
			if err := clearOutputDir(outputPath.Path); err != nil {
				return fmt.Errorf("清空 JS 输出目录失败 %s: %v", outputPath.Path, err)
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

// parsePluginOptions 解析 --frontend-api_opt：逗号分隔的 key=value（键名 snake_case，与常见 protoc 插件一致）。
// 合法键：typescript_outputs, javascript_outputs, types_from, service_import, service_import_js。
func parsePluginOptions(param string) (*PluginConfig, error) {
	config := &PluginConfig{
		ServiceImport:   "./api",
		ServiceImportJS: "",
		TypesRoot:       "@/api/proto-types",
		TsOut:           nil,
		JsOut:           nil,
	}

	if strings.TrimSpace(param) == "" {
		return config, nil
	}

	for _, pair := range strings.Split(param, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			return nil, fmt.Errorf("frontend-api: invalid option %q (want key=value)", pair)
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "" {
			return nil, fmt.Errorf("frontend-api: empty key in %q", pair)
		}

		switch key {
		case "typescript_outputs":
			if value == "" {
				return nil, fmt.Errorf("frontend-api: typescript_outputs= requires a value (semicolon-separated dirs, optional path:import per entry)")
			}
			config.TsOut = parseTargetPaths(value)
		case "javascript_outputs":
			if value == "" {
				return nil, fmt.Errorf("frontend-api: javascript_outputs= requires a value (semicolon-separated dirs, optional path:import per entry)")
			}
			config.JsOut = parseTargetPaths(value)
		case "types_from":
			if value == "" {
				return nil, fmt.Errorf("frontend-api: types_from= requires a value")
			}
			config.TypesRoot = value
		case "service_import":
			config.ServiceImport = value
		case "service_import_js":
			config.ServiceImportJS = value
		default:
			return nil, fmt.Errorf("frontend-api: unknown option %q", key)
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

// parseTargetPaths：分号分隔；每项可为 `dir` 或 `dir:override_service_import`。
func parseTargetPaths(value string) []OutputPathConfig {
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

	if len(config.TsOut) == 0 && len(config.JsOut) == 0 {
		return nil
	}

	for _, outputPathConfig := range config.TsOut {
		// 确定该路径使用的 service_import
		serviceImport := outputPathConfig.ServiceImport
		if serviceImport == "" {
			serviceImport = config.ServiceImport
		}

		// 准备模板数据
		data := ServiceInfo{
			ServiceName:   serviceName,
			ApiFileName:   apiFileName,
			Methods:       methods,
			ServiceImport: serviceImport,
			TypesRoot:     config.TypesRoot,
			TypeImports:   typeImports,
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

	for _, outputPathConfig := range config.JsOut {
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
		// 按目标目录所属项目的 prettier 配置生成，避免生成后被编辑器/lint-staged 重新格式化
		code := generateJavaScriptCode(data, resolvePrettierConfig(outputPathConfig.Path))
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
			fullImportPath := data.TypesRoot
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
		buf.WriteString(", opts?: object): Promise<")
		buf.WriteString(method.ResponseType)
		buf.WriteString("> =>\n")
		buf.WriteString("    service.")
		buf.WriteString(method.HttpMethod)
		buf.WriteString("('")
		buf.WriteString(method.HttpPath)
		buf.WriteString("', data, opts)")

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

// prettierConfig 收集生成 JS 时需要遵循的 prettier 选项（只取本生成器用得到的几项）。
type prettierConfig struct {
	Semi          bool   // 行尾分号
	SingleQuote   bool   // 单引号
	TabWidth      int    // 缩进宽度
	PrintWidth    int    // 单行最大宽度（超出则在 => 后换行）
	TrailingComma string // "none" | "es5" | "all"：是否给对象最后一个属性补尾逗号
}

// prettier v3 默认值：semi=true, singleQuote=false, tabWidth=2, printWidth=80, trailingComma="all"。
// 找不到任何 .prettierrc 时回退到这里，输出即与 prettier 默认风格一致。
func defaultPrettierConfig() prettierConfig {
	return prettierConfig{Semi: true, SingleQuote: false, TabWidth: 2, PrintWidth: 80, TrailingComma: "all"}
}

// prettierRaw 用指针字段区分「未设置」与「显式设为零值」，便于只覆盖出现过的键。
type prettierRaw struct {
	Semi          *bool   `json:"semi"`
	SingleQuote   *bool   `json:"singleQuote"`
	TabWidth      *int    `json:"tabWidth"`
	PrintWidth    *int    `json:"printWidth"`
	TrailingComma *string `json:"trailingComma"`
}

// resolvePrettierConfig 从输出目录向上逐级查找 prettier 配置（.prettierrc.json / .prettierrc / package.json 的 prettier 字段），
// 命中第一个即停止。找不到则返回 prettier 默认值。
func resolvePrettierConfig(outputDir string) prettierConfig {
	cfg := defaultPrettierConfig()
	dir, err := filepath.Abs(outputDir)
	if err != nil {
		return cfg
	}
	for {
		if raw, ok := readPrettierRaw(dir); ok {
			applyPrettierRaw(&cfg, raw)
			return cfg
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return cfg
		}
		dir = parent
	}
}

// readPrettierRaw 在单个目录里按 prettier 的优先级尝试读取配置，只有真正含 prettier 选项时才返回 ok=true。
func readPrettierRaw(dir string) (prettierRaw, bool) {
	// .prettierrc.json 与 .prettierrc（本仓库均为 JSON 内容）直接整体反序列化
	for _, name := range []string{".prettierrc.json", ".prettierrc"} {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			var raw prettierRaw
			if json.Unmarshal(b, &raw) == nil {
				return raw, true
			}
		}
	}
	// package.json 的 prettier 字段（不含该字段则视为未命中，继续向上找）
	if b, err := os.ReadFile(filepath.Join(dir, "package.json")); err == nil {
		var pkg struct {
			Prettier *prettierRaw `json:"prettier"`
		}
		if json.Unmarshal(b, &pkg) == nil && pkg.Prettier != nil {
			return *pkg.Prettier, true
		}
	}
	return prettierRaw{}, false
}

// applyPrettierRaw 把出现过的键覆盖到 cfg 上。
func applyPrettierRaw(cfg *prettierConfig, raw prettierRaw) {
	if raw.Semi != nil {
		cfg.Semi = *raw.Semi
	}
	if raw.SingleQuote != nil {
		cfg.SingleQuote = *raw.SingleQuote
	}
	if raw.TabWidth != nil && *raw.TabWidth > 0 {
		cfg.TabWidth = *raw.TabWidth
	}
	if raw.PrintWidth != nil && *raw.PrintWidth > 0 {
		cfg.PrintWidth = *raw.PrintWidth
	}
	if raw.TrailingComma != nil {
		cfg.TrailingComma = *raw.TrailingComma
	}
}

// generateJavaScriptCode 按 addressApi.js 风格生成 JS：无类型 import，(data, opts) => service.{method}('path', data, opts)。
// 输出严格对齐目标项目的 prettier 配置（缩进/分号/引号/尾逗号/行宽换行），避免生成后被重新格式化。
// opts 透传给 request 层（per-call 选项：toast/dedupe 等）。
func generateJavaScriptCode(data ServiceInfo, cfg prettierConfig) []byte {
	quote := "\""
	if cfg.SingleQuote {
		quote = "'"
	}
	semi := ""
	if cfg.Semi {
		semi = ";"
	}
	indent := strings.Repeat(" ", cfg.TabWidth)
	contIndent := strings.Repeat(" ", cfg.TabWidth*2) // 换行后调用体多缩进一级
	// es5/all 都会给多行对象的最后一个属性补尾逗号；none 不补
	lastComma := cfg.TrailingComma != "none"

	q := func(s string) string { return quote + s + quote }

	var buf bytes.Buffer
	buf.WriteString("import service from " + q(data.ServiceImport) + semi + "\n\n")
	buf.WriteString("export const " + data.ApiFileName + " = {\n")
	for i, method := range data.Methods {
		comma := ","
		if i == len(data.Methods)-1 && !lastComma {
			comma = ""
		}
		head := indent + method.MethodName + ": (data, opts) => "
		call := "service." + method.HttpMethod + "(" + q(method.HttpPath) + ", data, opts)"
		if len(head)+len(call)+len(comma) > cfg.PrintWidth {
			// 超出行宽：prettier 会在 => 后换行
			buf.WriteString(indent + method.MethodName + ": (data, opts) =>\n")
			buf.WriteString(contIndent + call + comma + "\n")
		} else {
			buf.WriteString(head + call + comma + "\n")
		}
	}
	buf.WriteString("}" + semi + "\n\n")
	buf.WriteString("export default " + data.ApiFileName + semi + "\n")
	return buf.Bytes()
}
