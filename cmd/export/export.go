package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/luanti-golang/mapparser"
)

// maximum line length (hex-encoded mapblock), mapblocks with lots of metadata can be large
const maxLineSize = 256 << 20

// sqlite3 -header -csv map.sqlite "select pos, hex(data) from blocks" > map.csv
func main() {
	file, err := os.Open("map.csv")
	if err != nil {
		panic(err)
	}
	defer file.Close()

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 1<<20), maxLineSize)

	line_num := 0
	for sc.Scan() {
		line_num++
		line := sc.Text()
		pos_str, hex_str, ok := strings.Cut(line, ",")
		if !ok {
			panic(fmt.Errorf("invalid format @ %d", line_num))
		}

		pos, err := strconv.ParseInt(pos_str, 10, 64)
		if err != nil {
			if line_num == 1 {
				// csv header
				continue
			}
			panic(fmt.Errorf("invalid position @ %d: %w", line_num, err))
		}

		b, err := hex.DecodeString(hex_str)
		if err != nil {
			panic(fmt.Errorf("invalid hex @ %d: %w", line_num, err))
		}

		mb, err := mapparser.Parse(b)
		if err != nil {
			panic(fmt.Errorf("parse error @ %d: %w", line_num, err))
		}

		x, y, z := PlainToCoord(pos)
		txt, err := json.Marshal(mb)
		if err != nil {
			panic(err)
		}

		fmt.Fprintf(out, "%d;%d;%d;%s\n", x, y, z, txt)
	}

	if err := sc.Err(); err != nil {
		panic(fmt.Errorf("read error after line %d: %w", line_num, err))
	}
}
