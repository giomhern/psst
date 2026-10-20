package sqlite

import (
	"bytes"
	"encoding/binary"
	"unicode/utf8"
)

const maxAttributedBodyBytes = 64 << 10

// decodeAttributedBody extracts only the first backing NSString from supported
// macOS v4, little-endian, system-1000 attributed-string archives. It follows
// the root's class chain and typed string field; it never searches arbitrary
// metadata for text. Attribute runs after the string are deliberately not parsed.
func decodeAttributedBody(data []byte) (string, bool) {
	if len(data) > maxAttributedBodyBytes {
		return "", false
	}
	reader := bodyReader{data: data}
	if !reader.consume("\x04\x0bstreamtyped\x81\xe8\x03\x84\x01@\x84") {
		return "", false
	}
	mutable := reader.consume("\x84\x84\x19NSMutableAttributedString\x00")
	if !reader.consume("\x84\x84\x12NSAttributedString\x00\x84\x84\x08NSObject\x00\x85") {
		return "", false
	}
	// The root's '@' type is shared-string reference zero (0x92).
	if !reader.consume("\x92\x84") {
		return "", false
	}
	reader.consume("\x84\x84\x0fNSMutableString\x01")
	if !reader.consume("\x84\x84\x08NSString\x01") {
		return "", false
	}
	// Object/class references share a table. The optional mutable root class
	// shifts NSObject from reference two to reference three.
	superclass := "\x94"
	if mutable {
		superclass = "\x95"
	}
	if !reader.consume(superclass + "\x84\x01+") {
		return "", false
	}
	size, ok := reader.length()
	if !ok || size > len(reader.data) {
		return "", false
	}
	text := reader.data[:size]
	reader.data = reader.data[size:]
	if !reader.consume("\x86") || !utf8.Valid(text) {
		return "", false
	}
	return string(text), true
}

// A pointer receiver advances this reader's remaining byte slice. It owns no
// external resources and never allocates based on an unchecked archive length.
type bodyReader struct{ data []byte }

func (r *bodyReader) consume(prefix string) bool {
	rest, ok := bytes.CutPrefix(r.data, []byte(prefix))
	if ok {
		r.data = rest
	}
	return ok
}

func (r *bodyReader) length() (int, bool) {
	if len(r.data) == 0 {
		return 0, false
	}
	head := r.data[0]
	r.data = r.data[1:]
	var size uint32
	switch head {
	case 0x81:
		if len(r.data) < 2 {
			return 0, false
		}
		size = uint32(binary.LittleEndian.Uint16(r.data[:2]))
		r.data = r.data[2:]
	case 0x82:
		if len(r.data) < 4 {
			return 0, false
		}
		size = binary.LittleEndian.Uint32(r.data[:4])
		r.data = r.data[4:]
	default:
		if head >= 0x80 && head <= 0x91 {
			return 0, false
		}
		size = uint32(head)
	}
	if size > maxAttributedBodyBytes {
		return 0, false
	}
	return int(size), true
}
