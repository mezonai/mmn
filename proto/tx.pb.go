// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        v6.31.1
// source: tx.proto

package proto

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type TxRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Data          []byte                 `protobuf:"bytes,1,opt,name=data,proto3" json:"data,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *TxRequest) Reset() {
	*x = TxRequest{}
	mi := &file_tx_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *TxRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TxRequest) ProtoMessage() {}

func (x *TxRequest) ProtoReflect() protoreflect.Message {
	mi := &file_tx_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TxRequest.ProtoReflect.Descriptor instead.
func (*TxRequest) Descriptor() ([]byte, []int) {
	return file_tx_proto_rawDescGZIP(), []int{0}
}

func (x *TxRequest) GetData() []byte {
	if x != nil {
		return x.Data
	}
	return nil
}

type TxResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Ok            bool                   `protobuf:"varint,1,opt,name=ok,proto3" json:"ok,omitempty"`
	Error         string                 `protobuf:"bytes,2,opt,name=error,proto3" json:"error,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *TxResponse) Reset() {
	*x = TxResponse{}
	mi := &file_tx_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *TxResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*TxResponse) ProtoMessage() {}

func (x *TxResponse) ProtoReflect() protoreflect.Message {
	mi := &file_tx_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use TxResponse.ProtoReflect.Descriptor instead.
func (*TxResponse) Descriptor() ([]byte, []int) {
	return file_tx_proto_rawDescGZIP(), []int{1}
}

func (x *TxResponse) GetOk() bool {
	if x != nil {
		return x.Ok
	}
	return false
}

func (x *TxResponse) GetError() string {
	if x != nil {
		return x.Error
	}
	return ""
}

var File_tx_proto protoreflect.FileDescriptor

const file_tx_proto_rawDesc = "" +
	"\n" +
	"\btx.proto\x12\x03mmn\"\x1f\n" +
	"\tTxRequest\x12\x12\n" +
	"\x04data\x18\x01 \x01(\fR\x04data\"2\n" +
	"\n" +
	"TxResponse\x12\x0e\n" +
	"\x02ok\x18\x01 \x01(\bR\x02ok\x12\x14\n" +
	"\x05error\x18\x02 \x01(\tR\x05error2;\n" +
	"\tTxService\x12.\n" +
	"\vTxBroadcast\x12\x0e.mmn.TxRequest\x1a\x0f.mmn.TxResponseB\x11Z\x0fmmn/proto;protob\x06proto3"

var (
	file_tx_proto_rawDescOnce sync.Once
	file_tx_proto_rawDescData []byte
)

func file_tx_proto_rawDescGZIP() []byte {
	file_tx_proto_rawDescOnce.Do(func() {
		file_tx_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_tx_proto_rawDesc), len(file_tx_proto_rawDesc)))
	})
	return file_tx_proto_rawDescData
}

var file_tx_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_tx_proto_goTypes = []any{
	(*TxRequest)(nil),  // 0: mmn.TxRequest
	(*TxResponse)(nil), // 1: mmn.TxResponse
}
var file_tx_proto_depIdxs = []int32{
	0, // 0: mmn.TxService.TxBroadcast:input_type -> mmn.TxRequest
	1, // 1: mmn.TxService.TxBroadcast:output_type -> mmn.TxResponse
	1, // [1:2] is the sub-list for method output_type
	0, // [0:1] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_tx_proto_init() }
func file_tx_proto_init() {
	if File_tx_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_tx_proto_rawDesc), len(file_tx_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_tx_proto_goTypes,
		DependencyIndexes: file_tx_proto_depIdxs,
		MessageInfos:      file_tx_proto_msgTypes,
	}.Build()
	File_tx_proto = out.File
	file_tx_proto_goTypes = nil
	file_tx_proto_depIdxs = nil
}
