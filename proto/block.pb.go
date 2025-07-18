// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.6
// 	protoc        v6.31.1
// source: proto/block.proto

package proto

import (
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"

	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	timestamppb "google.golang.org/protobuf/types/known/timestamppb"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type Entry struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	NumHashes     uint64                 `protobuf:"varint,1,opt,name=num_hashes,json=numHashes,proto3" json:"num_hashes,omitempty"`
	Hash          []byte                 `protobuf:"bytes,2,opt,name=hash,proto3" json:"hash,omitempty"`
	Transactions  [][]byte               `protobuf:"bytes,3,rep,name=transactions,proto3" json:"transactions,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Entry) Reset() {
	*x = Entry{}
	mi := &file_proto_block_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Entry) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Entry) ProtoMessage() {}

func (x *Entry) ProtoReflect() protoreflect.Message {
	mi := &file_proto_block_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Entry.ProtoReflect.Descriptor instead.
func (*Entry) Descriptor() ([]byte, []int) {
	return file_proto_block_proto_rawDescGZIP(), []int{0}
}

func (x *Entry) GetNumHashes() uint64 {
	if x != nil {
		return x.NumHashes
	}
	return 0
}

func (x *Entry) GetHash() []byte {
	if x != nil {
		return x.Hash
	}
	return nil
}

func (x *Entry) GetTransactions() [][]byte {
	if x != nil {
		return x.Transactions
	}
	return nil
}

type Block struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Slot          uint64                 `protobuf:"varint,1,opt,name=slot,proto3" json:"slot,omitempty"`
	PrevHash      []byte                 `protobuf:"bytes,2,opt,name=prev_hash,json=prevHash,proto3" json:"prev_hash,omitempty"`
	Entries       []*Entry               `protobuf:"bytes,3,rep,name=entries,proto3" json:"entries,omitempty"`
	LeaderId      string                 `protobuf:"bytes,4,opt,name=leader_id,json=leaderId,proto3" json:"leader_id,omitempty"`
	Timestamp     *timestamppb.Timestamp `protobuf:"bytes,5,opt,name=timestamp,proto3" json:"timestamp,omitempty"`
	Hash          []byte                 `protobuf:"bytes,6,opt,name=hash,proto3" json:"hash,omitempty"`
	Signature     []byte                 `protobuf:"bytes,7,opt,name=signature,proto3" json:"signature,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *Block) Reset() {
	*x = Block{}
	mi := &file_proto_block_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *Block) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*Block) ProtoMessage() {}

func (x *Block) ProtoReflect() protoreflect.Message {
	mi := &file_proto_block_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use Block.ProtoReflect.Descriptor instead.
func (*Block) Descriptor() ([]byte, []int) {
	return file_proto_block_proto_rawDescGZIP(), []int{1}
}

func (x *Block) GetSlot() uint64 {
	if x != nil {
		return x.Slot
	}
	return 0
}

func (x *Block) GetPrevHash() []byte {
	if x != nil {
		return x.PrevHash
	}
	return nil
}

func (x *Block) GetEntries() []*Entry {
	if x != nil {
		return x.Entries
	}
	return nil
}

func (x *Block) GetLeaderId() string {
	if x != nil {
		return x.LeaderId
	}
	return ""
}

func (x *Block) GetTimestamp() *timestamppb.Timestamp {
	if x != nil {
		return x.Timestamp
	}
	return nil
}

func (x *Block) GetHash() []byte {
	if x != nil {
		return x.Hash
	}
	return nil
}

func (x *Block) GetSignature() []byte {
	if x != nil {
		return x.Signature
	}
	return nil
}

type BroadcastResponse struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Ok            bool                   `protobuf:"varint,1,opt,name=ok,proto3" json:"ok,omitempty"`
	Error         string                 `protobuf:"bytes,2,opt,name=error,proto3" json:"error,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *BroadcastResponse) Reset() {
	*x = BroadcastResponse{}
	mi := &file_proto_block_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *BroadcastResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*BroadcastResponse) ProtoMessage() {}

func (x *BroadcastResponse) ProtoReflect() protoreflect.Message {
	mi := &file_proto_block_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use BroadcastResponse.ProtoReflect.Descriptor instead.
func (*BroadcastResponse) Descriptor() ([]byte, []int) {
	return file_proto_block_proto_rawDescGZIP(), []int{2}
}

func (x *BroadcastResponse) GetOk() bool {
	if x != nil {
		return x.Ok
	}
	return false
}

func (x *BroadcastResponse) GetError() string {
	if x != nil {
		return x.Error
	}
	return ""
}

type SubscribeRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	FollowerId    string                 `protobuf:"bytes,1,opt,name=follower_id,json=followerId,proto3" json:"follower_id,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *SubscribeRequest) Reset() {
	*x = SubscribeRequest{}
	mi := &file_proto_block_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *SubscribeRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*SubscribeRequest) ProtoMessage() {}

func (x *SubscribeRequest) ProtoReflect() protoreflect.Message {
	mi := &file_proto_block_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use SubscribeRequest.ProtoReflect.Descriptor instead.
func (*SubscribeRequest) Descriptor() ([]byte, []int) {
	return file_proto_block_proto_rawDescGZIP(), []int{3}
}

func (x *SubscribeRequest) GetFollowerId() string {
	if x != nil {
		return x.FollowerId
	}
	return ""
}

var File_proto_block_proto protoreflect.FileDescriptor

const file_proto_block_proto_rawDesc = "" +
	"\n" +
	"\x11proto/block.proto\x12\x03mmn\x1a\x1fgoogle/protobuf/timestamp.proto\"^\n" +
	"\x05Entry\x12\x1d\n" +
	"\n" +
	"num_hashes\x18\x01 \x01(\x04R\tnumHashes\x12\x12\n" +
	"\x04hash\x18\x02 \x01(\fR\x04hash\x12\"\n" +
	"\ftransactions\x18\x03 \x03(\fR\ftransactions\"\xe7\x01\n" +
	"\x05Block\x12\x12\n" +
	"\x04slot\x18\x01 \x01(\x04R\x04slot\x12\x1b\n" +
	"\tprev_hash\x18\x02 \x01(\fR\bprevHash\x12$\n" +
	"\aentries\x18\x03 \x03(\v2\n" +
	".mmn.EntryR\aentries\x12\x1b\n" +
	"\tleader_id\x18\x04 \x01(\tR\bleaderId\x128\n" +
	"\ttimestamp\x18\x05 \x01(\v2\x1a.google.protobuf.TimestampR\ttimestamp\x12\x12\n" +
	"\x04hash\x18\x06 \x01(\fR\x04hash\x12\x1c\n" +
	"\tsignature\x18\a \x01(\fR\tsignature\"9\n" +
	"\x11BroadcastResponse\x12\x0e\n" +
	"\x02ok\x18\x01 \x01(\bR\x02ok\x12\x14\n" +
	"\x05error\x18\x02 \x01(\tR\x05error\"3\n" +
	"\x10SubscribeRequest\x12\x1f\n" +
	"\vfollower_id\x18\x01 \x01(\tR\n" +
	"followerId2q\n" +
	"\fBlockService\x12/\n" +
	"\tBroadcast\x12\n" +
	".mmn.Block\x1a\x16.mmn.BroadcastResponse\x120\n" +
	"\tSubscribe\x12\x15.mmn.SubscribeRequest\x1a\n" +
	".mmn.Block0\x01B\x11Z\x0fmmn/proto;protob\x06proto3"

var (
	file_proto_block_proto_rawDescOnce sync.Once
	file_proto_block_proto_rawDescData []byte
)

func file_proto_block_proto_rawDescGZIP() []byte {
	file_proto_block_proto_rawDescOnce.Do(func() {
		file_proto_block_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_proto_block_proto_rawDesc), len(file_proto_block_proto_rawDesc)))
	})
	return file_proto_block_proto_rawDescData
}

var file_proto_block_proto_msgTypes = make([]protoimpl.MessageInfo, 4)
var file_proto_block_proto_goTypes = []any{
	(*Entry)(nil),                 // 0: mmn.Entry
	(*Block)(nil),                 // 1: mmn.Block
	(*BroadcastResponse)(nil),     // 2: mmn.BroadcastResponse
	(*SubscribeRequest)(nil),      // 3: mmn.SubscribeRequest
	(*timestamppb.Timestamp)(nil), // 4: google.protobuf.Timestamp
}
var file_proto_block_proto_depIdxs = []int32{
	0, // 0: mmn.Block.entries:type_name -> mmn.Entry
	4, // 1: mmn.Block.timestamp:type_name -> google.protobuf.Timestamp
	1, // 2: mmn.BlockService.Broadcast:input_type -> mmn.Block
	3, // 3: mmn.BlockService.Subscribe:input_type -> mmn.SubscribeRequest
	2, // 4: mmn.BlockService.Broadcast:output_type -> mmn.BroadcastResponse
	1, // 5: mmn.BlockService.Subscribe:output_type -> mmn.Block
	4, // [4:6] is the sub-list for method output_type
	2, // [2:4] is the sub-list for method input_type
	2, // [2:2] is the sub-list for extension type_name
	2, // [2:2] is the sub-list for extension extendee
	0, // [0:2] is the sub-list for field type_name
}

func init() { file_proto_block_proto_init() }
func file_proto_block_proto_init() {
	if File_proto_block_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_proto_block_proto_rawDesc), len(file_proto_block_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   4,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_proto_block_proto_goTypes,
		DependencyIndexes: file_proto_block_proto_depIdxs,
		MessageInfos:      file_proto_block_proto_msgTypes,
	}.Build()
	File_proto_block_proto = out.File
	file_proto_block_proto_goTypes = nil
	file_proto_block_proto_depIdxs = nil
}
