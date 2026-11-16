package mapparser

import (
	"encoding/binary"
	"fmt"

	"github.com/luanti-golang/types"
)

const MapDataSize = 16384

func parseMapdata(rawdata []byte, mapblock *types.MapBlock) error {
	if len(rawdata) < MapDataSize {
		return fmt.Errorf("mapdata length invalid: %d", len(rawdata))
	}

	// fixed-size sub-slices let the compiler drop per-iteration bounds checks
	content := rawdata[0 : 4096*2]
	param1 := rawdata[4096*2 : 4096*3]
	param2 := rawdata[4096*3 : 4096*4]

	contentId := make([]int, 4096)
	p1 := make([]int, 4096)
	p2 := make([]int, 4096)

	for i := 0; i < 4096; i++ {
		contentId[i] = int(binary.BigEndian.Uint16(content[i*2:]))
		p1[i] = int(param1[i])
		p2[i] = int(param2[i])
	}

	mapblock.ContentId = contentId
	mapblock.Param1 = p1
	mapblock.Param2 = p2

	return nil
}
