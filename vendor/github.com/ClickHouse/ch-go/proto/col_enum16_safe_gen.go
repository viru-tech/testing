//go:build !(amd64 || arm64) || purego

// Code generated by ./cmd/ch-gen-col, DO NOT EDIT.

package proto

import (
	"encoding/binary"

	"github.com/go-faster/errors"
)

var _ = binary.LittleEndian // clickHouse uses LittleEndian

// DecodeColumn decodes Enum16 rows from *Reader.
func (c *ColEnum16) DecodeColumn(r *Reader, rows int) error {
	if rows == 0 {
		return nil
	}
	const size = 16 / 8
	data, err := r.ReadRaw(rows * size)
	if err != nil {
		return errors.Wrap(err, "read")
	}
	v := *c
	// Move bound check out of loop.
	//
	// See https://github.com/golang/go/issues/30945.
	_ = data[len(data)-size]
	for i := 0; i <= len(data)-size; i += size {
		v = append(v,
			Enum16(binary.LittleEndian.Uint16(data[i:i+size])),
		)
	}
	*c = v
	return nil
}

// EncodeColumn encodes Enum16 rows to *Buffer.
func (c ColEnum16) EncodeColumn(b *Buffer) {
	v := c
	if len(v) == 0 {
		return
	}
	const size = 16 / 8
	offset := len(b.Buf)
	b.Buf = append(b.Buf, make([]byte, size*len(v))...)
	for _, vv := range v {
		binary.LittleEndian.PutUint16(
			b.Buf[offset:offset+size],
			uint16(vv),
		)
		offset += size
	}
}
