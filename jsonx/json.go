package jsonx

import (
	"io"

	jsoniter "github.com/json-iterator/go"
)

var jsonx = jsoniter.ConfigCompatibleWithStandardLibrary

func Marshal(v interface{}) ([]byte, error) {
	return jsonx.Marshal(v)
}

func Unmarshal(data []byte, v interface{}) error {
	return jsonx.Unmarshal(data, v)
}

func NewDecoder(r io.Reader) *jsoniter.Decoder {
	return jsonx.NewDecoder(r)
}

func NewEncoder(w io.Writer) *jsoniter.Encoder {
	return jsonx.NewEncoder(w)
}