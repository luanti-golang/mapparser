package mapparser

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/luanti-golang/types"
)

func readTestdata(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestParseZstdHeader(t *testing.T) {
	mb, err := Parse(readTestdata(t, "zstd-block.bin"))
	if err != nil {
		t.Fatal(err)
	}
	// flags byte is 0x02 (not underground), followed by lighting_complete 0xffff
	if mb.Underground {
		t.Error("expected Underground=false (flags read from wrong offset?)")
	}
	// BLOCK_TIMESTAMP_UNDEFINED
	if mb.Timestamp != 0xffffffff {
		t.Errorf("unexpected timestamp: %d", mb.Timestamp)
	}
}

func TestParseLegacyTimestamp(t *testing.T) {
	for file, expected := range map[string]uint32{
		"0.0.0":                12942709,
		"mb-with-metadata.bin": 164,
	} {
		mb, err := Parse(readTestdata(t, file))
		if err != nil {
			t.Fatal(err)
		}
		if mb.Timestamp != expected {
			t.Errorf("%s: timestamp %d, expected %d", file, mb.Timestamp, expected)
		}
		if !mb.Underground {
			t.Errorf("%s: expected Underground=true", file)
		}
	}
}

// every prefix of every test block must parse without panicking
func TestParseTruncated(t *testing.T) {
	files, err := filepath.Glob("testdata/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		network := filepath.Base(file) == "network-blockdata.bin"
		for n := 0; n < len(data); n++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s truncated to %d bytes: panic: %v", file, n, r)
					}
				}()
				if network {
					ParseNetwork(28, data[:n])
				} else {
					Parse(data[:n])
				}
			}()
		}
	}
}

// the zstd body is a single frame, so any truncation must be reported
func TestParseZstdTruncatedErrors(t *testing.T) {
	data := readTestdata(t, "zstd-block.bin")
	for n := 1; n < len(data); n++ {
		if _, err := Parse(data[:n]); err == nil {
			t.Fatalf("no error for zstd block truncated to %d of %d bytes", n, len(data))
		}
	}
}

// truncating the disk-only trailer (static objects, timestamp, mapping) must be reported
func TestParseTrailerTruncatedErrors(t *testing.T) {
	data := readTestdata(t, "0.0.0")
	// the block ends with the node timer header (3 bytes, not parsed),
	// preceded by the name-id mapping and timestamp
	const nodeTimerHeader = 3
	for n := len(data) - nodeTimerHeader - 20; n < len(data)-nodeTimerHeader; n++ {
		if _, err := Parse(data[:n]); !errors.Is(err, ErrTruncated) {
			t.Fatalf("truncated to %d of %d bytes: expected ErrTruncated, got %v", n, len(data), err)
		}
	}
}

func TestParseCorruptZlib(t *testing.T) {
	data := bytes.Clone(readTestdata(t, "0.0.0"))
	// the mapdata zlib stream starts at offset 6; corrupt its compressed body
	for i := 10; i < 20; i++ {
		data[i] ^= 0xff
	}
	if _, err := Parse(data); err == nil {
		t.Error("expected error for corrupt zlib stream")
	}
}

func zlibCompress(t testing.TB, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	w.Write(data)
	w.Close()
	return buf.Bytes()
}

func TestDecompressZlib(t *testing.T) {
	payload := bytes.Repeat([]byte("luanti"), 5000)
	stream := zlibCompress(t, payload)
	trailer := []byte{1, 2, 3, 4, 5}

	// consumed length must be exact even with trailing data (several rounds to exercise the pool)
	for i := 0; i < 3; i++ {
		out, n, err := decompressZlib(append(bytes.Clone(stream), trailer...), 0)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(out, payload) {
			t.Fatal("payload mismatch")
		}
		if n != len(stream) {
			t.Fatalf("consumed %d, expected %d", n, len(stream))
		}
	}

	// truncated stream
	if _, _, err := decompressZlib(stream[:len(stream)/2], 0); err == nil {
		t.Error("expected error for truncated stream")
	}

	// adler32 checksum mismatch
	bad := bytes.Clone(stream)
	bad[len(bad)-1] ^= 0xff
	if _, _, err := decompressZlib(bad, 0); err == nil {
		t.Error("expected error for checksum mismatch")
	}

	// invalid header, then a valid stream through the same pool
	if _, _, err := decompressZlib([]byte{0, 0, 0}, 0); err == nil {
		t.Error("expected error for invalid header")
	}
	if out, _, err := decompressZlib(stream, 0); err != nil || !bytes.Equal(out, payload) {
		t.Errorf("pooled reader broken after error: %v", err)
	}
}

// metadata builder for hand-crafted test cases

type testNodeMeta struct {
	pos       uint16
	fields    [][2]string
	inventory string
}

func buildMetadata(version byte, nodes ...testNodeMeta) []byte {
	var b bytes.Buffer
	b.WriteByte(version)
	binary.Write(&b, binary.BigEndian, uint16(len(nodes)))
	for _, n := range nodes {
		binary.Write(&b, binary.BigEndian, n.pos)
		binary.Write(&b, binary.BigEndian, uint32(len(n.fields)))
		for _, f := range n.fields {
			binary.Write(&b, binary.BigEndian, uint16(len(f[0])))
			b.WriteString(f[0])
			binary.Write(&b, binary.BigEndian, uint32(len(f[1])))
			b.WriteString(f[1])
			if version >= 2 {
				b.WriteByte(0) // private flag
			}
		}
		b.WriteString(n.inventory)
	}
	return b.Bytes()
}

func parseTestMetadata(data []byte) (*types.MapBlock, error) {
	mb := types.NewMapblock()
	return mb, parseMetadata(&cursor{data: data}, mb)
}

func TestParseMetadataMultipleNodes(t *testing.T) {
	data := buildMetadata(2,
		testNodeMeta{
			pos:       1,
			fields:    [][2]string{{"infotext", "Chest"}, {"formspec", "size[8,9]"}},
			inventory: "List main 3\nWidth 0\nItem default:stone 99\nEmpty\nItem default:dirt\nEndInventoryList\nList empty 0\nWidth 0\nEndInventoryList\nEndInventory\n",
		},
		testNodeMeta{
			pos:       4095,
			fields:    [][2]string{{"owner", "singleplayer"}},
			inventory: "List src 1\nWidth 0\nItem default:tree\nEndInventoryList\nEndInventory\n",
		},
		testNodeMeta{
			pos:       42,
			inventory: "EndInventory\n",
		},
	)
	mb, err := parseTestMetadata(data)
	if err != nil {
		t.Fatal(err)
	}

	if mb.Fields[1]["infotext"] != "Chest" || mb.Fields[1]["formspec"] != "size[8,9]" {
		t.Errorf("node 1 fields: %v", mb.Fields[1])
	}
	main := mb.Inventory[1]["main"]
	if len(main) != 3 || main[0] != "default:stone 99" || main[1] != "" || main[2] != "default:dirt" {
		t.Errorf("node 1 main inventory: %q", main)
	}
	if empty, ok := mb.Inventory[1]["empty"]; !ok || empty == nil || len(empty) != 0 {
		t.Errorf("node 1 empty inventory: %#v", empty)
	}

	if mb.Fields[4095]["owner"] != "singleplayer" {
		t.Errorf("node 4095 fields: %v", mb.Fields[4095])
	}
	if src := mb.Inventory[4095]["src"]; len(src) != 1 || src[0] != "default:tree" {
		t.Errorf("node 4095 src inventory: %q", src)
	}

	if _, ok := mb.Fields[42]; !ok {
		t.Error("node 42 missing")
	}
	if _, ok := mb.Inventory[42]; ok {
		t.Error("node 42 should have no inventory")
	}
}

func TestParseMetadataVersion1(t *testing.T) {
	data := buildMetadata(1,
		testNodeMeta{pos: 7, fields: [][2]string{{"a", "b"}}, inventory: "EndInventory\n"},
		testNodeMeta{pos: 8, fields: [][2]string{{"c", "d"}}, inventory: "EndInventory\n"},
	)
	mb, err := parseTestMetadata(data)
	if err != nil {
		t.Fatal(err)
	}
	if mb.Fields[7]["a"] != "b" || mb.Fields[8]["c"] != "d" {
		t.Errorf("fields: %v", mb.Fields)
	}
}

// lines longer than bufio.Scanner's 64KB limit used to silently corrupt the offset
func TestParseMetadataLongLine(t *testing.T) {
	longItem := "default:book 1 0 \"" + strings.Repeat("x", 100_000) + "\""
	data := buildMetadata(2,
		testNodeMeta{pos: 1, inventory: "List main 1\nWidth 0\nItem " + longItem + "\nEndInventoryList\nEndInventory\n"},
		testNodeMeta{pos: 2, fields: [][2]string{{"k", "v"}}, inventory: "EndInventory\n"},
	)
	mb, err := parseTestMetadata(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := mb.Inventory[1]["main"]; len(got) != 1 || got[0] != longItem {
		t.Error("long item not parsed")
	}
	if mb.Fields[2]["k"] != "v" {
		t.Errorf("node after long line misparsed: %v", mb.Fields[2])
	}
}

func TestParseMetadataCRLF(t *testing.T) {
	data := buildMetadata(2,
		testNodeMeta{pos: 1, inventory: "List main 1\r\nWidth 0\r\nItem default:stone\r\nEndInventoryList\r\nEndInventory\r\n"},
		testNodeMeta{pos: 2, fields: [][2]string{{"k", "v"}}, inventory: "EndInventory\n"},
	)
	mb, err := parseTestMetadata(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := mb.Inventory[1]["main"]; len(got) != 1 || got[0] != "default:stone" {
		t.Errorf("CRLF inventory: %q", got)
	}
	if mb.Fields[2]["k"] != "v" {
		t.Errorf("node after CRLF inventory misparsed: %v", mb.Fields[2])
	}
}

func TestParseMetadataErrors(t *testing.T) {
	tests := map[string][]byte{
		"empty":             {},
		"missing count":     {2},
		"unterminated inv":  buildMetadata(2, testNodeMeta{pos: 1, inventory: "List main 1\nItem default:stone\n"}),
		"no inventory":      buildMetadata(2, testNodeMeta{pos: 1, fields: [][2]string{{"a", "b"}}}),
		"list without name": buildMetadata(2, testNodeMeta{pos: 1, inventory: "List \nEndInventory\n"}),
		"garbage line":      buildMetadata(2, testNodeMeta{pos: 1, inventory: "garbage\nEndInventory\n"}),
		// claims 2^32-1 values: must fail fast instead of allocating
		"huge value count": {2, 0, 1, 0, 1, 0xff, 0xff, 0xff, 0xff, 0, 0},
		// value length beyond the end of the data
		"huge value length": {2, 0, 1, 0, 1, 0, 0, 0, 1, 0, 1, 'k', 0xff, 0xff, 0xff, 0xff},
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := parseTestMetadata(data); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func FuzzParse(f *testing.F) {
	files, _ := filepath.Glob("testdata/*")
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		mb, err := Parse(data)
		if err == nil && mb == nil {
			t.Fatal("nil mapblock without error")
		}
	})
}

func benchmarkParse(b *testing.B, file string) {
	data := readTestdata(b, file)
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseZlib(b *testing.B)         { benchmarkParse(b, "0.0.0") }
func BenchmarkParseZlibMetadata(b *testing.B) { benchmarkParse(b, "mb-with-metadata.bin") }
func BenchmarkParseZstd(b *testing.B)         { benchmarkParse(b, "zstd-block.bin") }

func BenchmarkParseZstdParallel(b *testing.B) {
	data := readTestdata(b, "zstd-block.bin")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := Parse(data); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
