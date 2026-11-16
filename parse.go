package mapparser

import (
	"errors"
	"fmt"

	"github.com/luanti-golang/types"
)

var ErrNoData = errors.New("no data")
var ErrMapblockVersion = errors.New("mapblock version unsupported")

// ParseNetwork parses a mapblock as sent over the network (without the leading version byte)
func ParseNetwork(ser_ver uint8, data []byte) (*types.MapBlock, error) {
	return parse(ser_ver, data, len(data)+1, true)
}

// Parse parses a serialized mapblock as stored in the map database
func Parse(data []byte) (*types.MapBlock, error) {
	if len(data) == 0 {
		return nil, ErrNoData
	}
	return parse(data[0], data[1:], len(data), false)
}

// parse parses the mapblock body (everything after the version byte)
func parse(version uint8, body []byte, size int, network bool) (*types.MapBlock, error) {
	mapblock := types.NewMapblock()
	mapblock.Size = size
	mapblock.Version = version

	if version < 25 || version > 29 {
		return mapblock, ErrMapblockVersion
	}

	var err error
	if version >= 29 {
		err = parseZstdBlock(body, mapblock)
	} else {
		err = parseZlibBlock(version, body, mapblock, network)
	}
	if err != nil {
		return nil, err
	}
	return mapblock, nil
}

// parseZlibBlock parses versions 25-28 (separately zlib-compressed mapdata and metadata)
func parseZlibBlock(version uint8, body []byte, mapblock *types.MapBlock, network bool) error {
	c := &cursor{data: body}

	flags, err := c.u8()
	if err != nil {
		return err
	}
	mapblock.Underground = (flags & 0x01) == 0x01

	if version >= 27 {
		// u16 lighting_complete
		if err = c.skip(2); err != nil {
			return err
		}
	}

	if err = parseWidths(c); err != nil {
		return err
	}

	// mapdata
	mapdata, n, err := decompressZlib(c.rest(), MapDataSize)
	if err != nil {
		return fmt.Errorf("mapdata: %w", err)
	}
	if err = parseMapdata(mapdata, mapblock); err != nil {
		return err
	}
	c.off += n

	// metadata
	metadata, n, err := decompressZlib(c.rest(), 0)
	if err != nil {
		return fmt.Errorf("metadata: %w", err)
	}
	if err = parseMetadata(&cursor{data: metadata}, mapblock); err != nil {
		return err
	}
	c.off += n

	// The network serialization ends after the metadata, optionally followed by
	// the network-specific version byte (2). On disk the static object list
	// follows, whose version byte is always 0.
	if network || c.remaining() == 0 || c.data[c.off] != 0 {
		return nil
	}

	if err = parseStaticObjects(c); err != nil {
		return err
	}

	timestamp, err := c.u32()
	if err != nil {
		return err
	}
	mapblock.Timestamp = timestamp

	// name-id mapping version
	if err = c.skip(1); err != nil {
		return err
	}
	return parseBlockMapping(c, mapblock)
}

// parseZstdBlock parses version 29+ (whole body zstd-compressed)
func parseZstdBlock(body []byte, mapblock *types.MapBlock) error {
	raw, err := decompressZstd(body)
	if err != nil {
		return err
	}
	c := &cursor{data: raw}

	flags, err := c.u8()
	if err != nil {
		return err
	}
	mapblock.Underground = (flags & 0x01) == 0x01

	// u16 lighting_complete
	if err = c.skip(2); err != nil {
		return err
	}

	timestamp, err := c.u32()
	if err != nil {
		return err
	}
	mapblock.Timestamp = timestamp

	// name-id mapping version
	if err = c.skip(1); err != nil {
		return err
	}
	if err = parseBlockMapping(c, mapblock); err != nil {
		return err
	}

	if err = parseWidths(c); err != nil {
		return err
	}

	mapdata, err := c.take(MapDataSize)
	if err != nil {
		return err
	}
	if err = parseMapdata(mapdata, mapblock); err != nil {
		return err
	}

	return parseMetadata(c, mapblock)
}

func parseWidths(c *cursor) error {
	content_width, err := c.u8()
	if err != nil {
		return err
	}
	params_width, err := c.u8()
	if err != nil {
		return err
	}
	if content_width != 2 {
		return fmt.Errorf("content_width unexpected: %d", content_width)
	}
	if params_width != 2 {
		return fmt.Errorf("params_width unexpected: %d", params_width)
	}
	return nil
}
