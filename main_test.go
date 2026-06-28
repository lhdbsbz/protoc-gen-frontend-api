package main

import (
	"reflect"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// 程序化构造一个自包含的 proto3 文件，覆盖各种 bytes 形态，用于测 collectBytesPaths。
func buildTestFile(t *testing.T) protoreflect.FileDescriptor {
	t.Helper()

	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	repeated := descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	tBytes := descriptorpb.FieldDescriptorProto_TYPE_BYTES.Enum()
	tString := descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()
	tMessage := descriptorpb.FieldDescriptorProto_TYPE_MESSAGE.Enum()

	field := func(name string, num int32, typ *descriptorpb.FieldDescriptorProto_Type, label *descriptorpb.FieldDescriptorProto_Label) *descriptorpb.FieldDescriptorProto {
		return &descriptorpb.FieldDescriptorProto{
			Name:     proto.String(name),
			Number:   proto.Int32(num),
			Type:     typ,
			Label:    label,
			JsonName: proto.String(name), // 测试里全用单词名，JSON 名即原名
		}
	}
	msgField := func(name string, num int32, typeName string, label *descriptorpb.FieldDescriptorProto_Label) *descriptorpb.FieldDescriptorProto {
		f := field(name, num, tMessage, label)
		f.TypeName = proto.String(typeName)
		return f
	}

	fd := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test.proto"),
		Package: proto.String("testpkg"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name:  proto.String("SttEvent"),
				Field: []*descriptorpb.FieldDescriptorProto{field("text", 1, tString, optional)},
			},
			{
				Name:  proto.String("AudioEvent"),
				Field: []*descriptorpb.FieldDescriptorProto{field("data", 1, tBytes, optional)},
			},
			{
				// oneof：audio 这一支含嵌套 bytes，stt 不含
				Name: proto.String("SessionResponse"),
				Field: []*descriptorpb.FieldDescriptorProto{
					func() *descriptorpb.FieldDescriptorProto {
						f := msgField("stt", 3, ".testpkg.SttEvent", optional)
						f.OneofIndex = proto.Int32(0)
						return f
					}(),
					func() *descriptorpb.FieldDescriptorProto {
						f := msgField("audio", 5, ".testpkg.AudioEvent", optional)
						f.OneofIndex = proto.Int32(0)
						return f
					}(),
				},
				OneofDecl: []*descriptorpb.OneofDescriptorProto{{Name: proto.String("event")}},
			},
			{
				Name:  proto.String("TopBytes"),
				Field: []*descriptorpb.FieldDescriptorProto{field("payload", 1, tBytes, optional)},
			},
			{
				Name:  proto.String("RepBytes"),
				Field: []*descriptorpb.FieldDescriptorProto{field("images", 2, tBytes, repeated)},
			},
			{
				// map<string,bytes> m = 1
				Name:  proto.String("MapBytes"),
				Field: []*descriptorpb.FieldDescriptorProto{msgField("m", 1, ".testpkg.MapBytes.MEntry", repeated)},
				NestedType: []*descriptorpb.DescriptorProto{
					{
						Name:    proto.String("MEntry"),
						Options: &descriptorpb.MessageOptions{MapEntry: proto.Bool(true)},
						Field: []*descriptorpb.FieldDescriptorProto{
							field("key", 1, tString, optional),
							field("value", 2, tBytes, optional),
						},
					},
				},
			},
			{
				// 自引用：blob(bytes) + next(Node)
				Name: proto.String("Node"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("blob", 1, tBytes, optional),
					msgField("next", 2, ".testpkg.Node", optional),
				},
			},
			{
				Name:  proto.String("Plain"),
				Field: []*descriptorpb.FieldDescriptorProto{field("x", 1, tString, optional)},
			},
		},
	}

	file, err := protodesc.NewFile(fd, protoregistry.GlobalFiles)
	if err != nil {
		t.Fatalf("protodesc.NewFile: %v", err)
	}
	return file
}

func TestCollectBytesPaths(t *testing.T) {
	file := buildTestFile(t)
	msgs := file.Messages()
	byName := map[string]protoreflect.MessageDescriptor{}
	for i := 0; i < msgs.Len(); i++ {
		m := msgs.Get(i)
		byName[string(m.Name())] = m
	}

	cases := []struct {
		msg  string
		want [][]string
	}{
		{"AudioEvent", [][]string{{"data"}}},
		{"TopBytes", [][]string{{"payload"}}},
		{"SessionResponse", [][]string{{"audio", "data"}}}, // oneof 内嵌套；stt 无 bytes
		{"RepBytes", [][]string{{"images"}}},               // repeated bytes 终止于字段
		{"MapBytes", [][]string{{"m"}}},                    // map<_,bytes> 终止于字段
		{"Node", [][]string{{"blob"}}},                     // 自引用：环阻断，仅顶层 blob
		{"Plain", nil},                                     // 无 bytes
	}

	for _, c := range cases {
		t.Run(c.msg, func(t *testing.T) {
			m, ok := byName[c.msg]
			if !ok {
				t.Fatalf("message %s not found", c.msg)
			}
			got := collectBytesPaths(m)
			if len(got) == 0 && len(c.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("collectBytesPaths(%s) = %v, want %v", c.msg, got, c.want)
			}
		})
	}
}

func TestBytesPathsToJSLiteral(t *testing.T) {
	if got := bytesPathsToJSLiteral(nil); got != "" {
		t.Errorf("empty paths => %q, want empty string", got)
	}
	got := bytesPathsToJSLiteral([][]string{{"audio", "data"}})
	want := `[["audio","data"]]`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBytesPathArgs(t *testing.T) {
	// 无 bytes：不追加实参
	if got := bytesPathArgs(MethodInfo{}); got != "" {
		t.Errorf("no bytes => %q, want empty", got)
	}
	// 仅响应侧有 bytes：请求侧补 []
	got := bytesPathArgs(MethodInfo{RespBytesPaths: `[["payload"]]`})
	want := `, [], [["payload"]]`
	if got != want {
		t.Errorf("resp-only => %q, want %q", got, want)
	}
	// 双侧都有
	got = bytesPathArgs(MethodInfo{ReqBytesPaths: `[["audio","data"]]`, RespBytesPaths: `[["audio","data"]]`})
	want = `, [["audio","data"]], [["audio","data"]]`
	if got != want {
		t.Errorf("both => %q, want %q", got, want)
	}
}
