package main

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"strings"

	"google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/compiler/protogen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// 插件配置
type PluginConfig struct {
	ServiceImport string // service 导入路径（相对路径，如 './api.js'）
	OutputDir     string // 输出目录路径（可选，如果提供则手动创建）
}

// 方法信息结构体
type MethodInfo struct {
	MethodName string // 方法名称
	HttpPath   string // HTTP 路径
	HttpMethod string // HTTP 方法（post, get等）
}

// 服务信息结构体
type ServiceInfo struct {
	ServiceName   string       // 服务名称（去掉 Service 后缀）
	ApiFileName   string       // API 文件名（如 productApi）
	Methods       []MethodInfo // 方法列表
	ServiceImport string       // service 导入路径
	Comment       string       // 服务注释
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

		// 如果配置中指定了输出目录，确保目录存在
		if config.OutputDir != "" {
			if err := os.MkdirAll(config.OutputDir, 0755); err != nil {
				return fmt.Errorf("创建输出目录失败: %v", err)
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
		ServiceImport: "./api.js", // 默认 service 导入路径
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
		case "output_dir":
			config.OutputDir = value
		}
	}

	return config, nil
}

// generateFrontendApi 生成前端 API 文件
func generateFrontendApi(gen *protogen.Plugin, file *protogen.File, service *protogen.Service, config *PluginConfig) error {
	// 确保输出目录存在（protogen 应该会自动创建，但为了保险，我们手动创建）
	// 注意：protogen 的输出目录是从 --frontend-api_out 参数获取的
	// 我们无法直接访问，但 NewGeneratedFile 会自动创建目录
	// 如果自动创建失败，我们会在写入时看到错误
	// 服务名称（去掉 Service 后缀）
	serviceName := strings.TrimSuffix(string(service.Desc.Name()), "Service")

	// 生成 API 文件名（例如：GoodsService -> goodsApi）
	apiFileName := toCamelCase(serviceName) + "Api"

	// 提取方法信息
	var methods []MethodInfo
	for _, method := range service.Methods {
		// 只处理有 HTTP 注解的方法
		if httpRule := extractHttpRule(method); httpRule != nil {
			methodInfo := MethodInfo{
				MethodName: string(method.Desc.Name()),
				HttpPath:   httpRule.Path,
				HttpMethod: strings.ToLower(httpRule.Method),
			}
			methods = append(methods, methodInfo)
		}
	}

	// 如果没有方法，跳过生成
	if len(methods) == 0 {
		return nil
	}

	// 尝试从 proto 文件中读取服务注释
	serviceComment := getServiceComment(service)

	// 准备模板数据
	data := ServiceInfo{
		ServiceName:   serviceName,
		ApiFileName:   apiFileName,
		Methods:       methods,
		ServiceImport: config.ServiceImport,
		Comment:       serviceComment,
	}

	// 生成代码
	var buf bytes.Buffer

	// 写入 import
	buf.WriteString("import service from '")
	buf.WriteString(config.ServiceImport)
	buf.WriteString("';\n\n")

	// 写入注释
	if data.Comment != "" {
		buf.WriteString("// ")
		buf.WriteString(data.Comment)
		buf.WriteString("\n")
	}

	// 写入 export
	buf.WriteString("export const ")
	buf.WriteString(data.ApiFileName)
	buf.WriteString(" = {\n")

	// 写入方法
	for i, method := range data.Methods {
		buf.WriteString("    ")
		buf.WriteString(method.MethodName)
		buf.WriteString(": (data) => service.")
		buf.WriteString(method.HttpMethod)
		buf.WriteString("('")
		buf.WriteString(method.HttpPath)
		buf.WriteString("', data)")

		if i < len(data.Methods)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}

	buf.WriteString("};\n\n")
	buf.WriteString("export default ")
	buf.WriteString(data.ApiFileName)
	buf.WriteString(";\n")

	// 生成文件名（使用小驼峰命名）
	fileName := toCamelCase(serviceName) + "Api.js"

	// protoc 会将 --frontend-api_out 指定的目录作为基础路径
	// 我们只需要指定文件名，protogen 会自动处理输出目录
	// 直接使用文件名，不使用额外的 output_dir 配置
	outputPath := fileName

	// 创建输出文件（protogen 会自动处理输出目录，即 --frontend-api_out 指定的目录）
	g := gen.NewGeneratedFile(outputPath, "")

	// 写入文件内容
	// protogen 会在写入时自动创建目录，如果目录不存在
	if _, err := g.Write(buf.Bytes()); err != nil {
		// 如果写入失败，可能是因为目录不存在
		// 如果配置中指定了输出目录，尝试手动创建
		if config.OutputDir != "" {
			if err := os.MkdirAll(config.OutputDir, 0755); err == nil {
				// 目录创建成功，重试写入
				if _, err := g.Write(buf.Bytes()); err != nil {
					return fmt.Errorf("写入文件失败: %v", err)
				}
				return nil
			}
		}
		return fmt.Errorf("写入文件失败: %v (请确保输出目录存在且有写权限)", err)
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

	// 使用 defer recover 来捕获可能的 panic
	defer func() {
		if r := recover(); r != nil {
			// 如果发生 panic，返回 nil
		}
	}()

	// 使用反射安全地访问 Pattern 字段
	ruleValue := reflect.ValueOf(rule).Elem()
	patternField := ruleValue.FieldByName("Pattern")
	if !patternField.IsValid() || patternField.IsNil() {
		return nil
	}

	// 优先使用 post/get/put/delete/patch 中的路径
	// 使用类型断言访问不同的 HTTP 方法
	patternInterface := patternField.Interface()
	if patternInterface == nil {
		return nil
	}

	switch v := patternInterface.(type) {
	case *annotations.HttpRule_Post:
		if v != nil && len(v.Post) > 0 {
			return &HttpRule{
				Method: "post",
				Path:   v.Post,
			}
		}
	case *annotations.HttpRule_Get:
		if v != nil && len(v.Get) > 0 {
			return &HttpRule{
				Method: "get",
				Path:   v.Get,
			}
		}
	case *annotations.HttpRule_Put:
		if v != nil && len(v.Put) > 0 {
			return &HttpRule{
				Method: "put",
				Path:   v.Put,
			}
		}
	case *annotations.HttpRule_Delete:
		if v != nil && len(v.Delete) > 0 {
			return &HttpRule{
				Method: "delete",
				Path:   v.Delete,
			}
		}
	case *annotations.HttpRule_Patch:
		if v != nil && len(v.Patch) > 0 {
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

// getServiceComment 获取服务注释
// 尝试从 proto 文件中读取服务注释，如果读取不到则返回空字符串
func getServiceComment(service *protogen.Service) string {
	// protogen 的 API 不直接提供读取注释的方法
	// 如果需要读取注释，需要使用 protoparse 或其他库
	// 作为公共插件，我们保持简单：如果无法读取注释，就不生成注释
	// 返回空字符串，不生成注释
	return ""
}
