package sqlite

import (
	"bytes"
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

func attributedFixture(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/attributed-" + name + ".bin")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// Change only the backing string field of a Foundation-generated archive.
// Attribute runs remain opaque to this text-only decoder.
func attributedText(t testing.TB, text []byte, lengthBytes int) []byte {
	t.Helper()
	base := attributedFixture(t, "unicode")
	field := bytes.Index(base, []byte{0x84, 1, '+'}) + 3
	if field < 3 {
		t.Fatal("fixture has no string field")
	}
	tail := base[field+1+int(base[field]):]
	data := append([]byte(nil), base[:field]...)
	switch lengthBytes {
	case 1:
		data = append(data, byte(len(text)))
	case 2:
		data = append(data, 0x81, byte(len(text)), byte(len(text)>>8))
	case 4:
		data = append(data, 0x82)
		data = binary.LittleEndian.AppendUint32(data, uint32(len(text)))
	default:
		t.Fatal("invalid test length size")
	}
	data = append(data, text...)
	return append(data, tail...)
}

func TestAttributedBodyFoundationArchives(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"unicode", "Hello synthetic 世界 🙂"},
		{"mutable", "Mutable synthetic message"},
		{"long", "Long synthetic. " + strings.Repeat("abc ", 71)},
		{"styled", "Styled synthetic caption \uFFFC end"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := decodeAttributedBody(attributedFixture(t, tc.name))
			if !ok || got != tc.want {
				t.Fatalf("decode = %q, %v; want %q", got, ok, tc.want)
			}
		})
	}
}

func TestAttributedBodyLengthEncodings(t *testing.T) {
	for _, tc := range []struct {
		name        string
		size, width int
	}{
		{"empty", 0, 1}, {"short", 10, 1}, {"direct boundary", 127, 1},
		{"reserved byte", 128, 2}, {"last reserved", 145, 2}, {"unsigned direct", 146, 1},
		{"unsigned maximum", 255, 1}, {"long", 300, 2}, {"four-byte field", 300, 4},
		{"unicode bound", 12003, 2}, {"large unsigned length", 40000, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := strings.Repeat("a", tc.size)
			got, ok := decodeAttributedBody(attributedText(t, []byte(want), tc.width))
			if !ok || got != want {
				t.Fatal("declared length was not decoded exactly")
			}
		})
	}
}

func TestAttributedBodyRejectsUnsupportedAndMalformedData(t *testing.T) {
	valid := attributedFixture(t, "unicode")
	field := bytes.Index(valid, []byte{0x84, 1, '+'}) + 3
	cases := [][]byte{nil, []byte("NSString readable metadata"), append(bytes.Repeat([]byte{0}, maxAttributedBodyBytes), valid...), attributedText(t, []byte{0xff, 0xfe}, 1)}
	for _, index := range []int{0, 2, 15, 41, 67, field} {
		corrupt := bytes.Clone(valid)
		corrupt[index] = 0x80
		cases = append(cases, corrupt)
	}
	for end := 0; end < field+1+int(valid[field])+1; end++ {
		cases = append(cases, valid[:end])
	}
	huge := attributedText(t, []byte("x"), 4)
	for i := field + 1; i < field+5; i++ {
		huge[i] = 0xff
	}
	cases = append(cases, huge)
	for i, data := range cases {
		if text, ok := decodeAttributedBody(data); ok || text != "" {
			t.Fatalf("case %d accepted unsupported data", i)
		}
	}
}

func FuzzDecodeAttributedBody(f *testing.F) {
	f.Add(attributedFixture(f, "unicode"))
	f.Add(attributedFixture(f, "mutable"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		text, ok := decodeAttributedBody(data)
		if !ok && text != "" {
			t.Fatal("failed decode returned text")
		}
		if ok && (!utf8.ValidString(text) || len(text) > maxAttributedBodyBytes || len(text) > len(data)) {
			t.Fatal("decoded text violated bounds")
		}
	})
}

func TestAttributedBodyPreservesTextControlsForDisplayBoundary(t *testing.T) {
	want := "First\nSecond\x00\x1b[31m\u202e🙂"
	got, ok := decodeAttributedBody(attributedText(t, []byte(want), 1))
	if !ok || got != want {
		t.Fatal("decoder altered text bytes; escaping belongs at the display boundary")
	}
}
