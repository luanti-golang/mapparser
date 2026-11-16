package mapparser

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/luanti-golang/types"
)

const (
	INVENTORY_TERMINATOR = "EndInventory"
	INVENTORY_END        = "EndInventoryList"
	INVENTORY_START      = "List"
	ITEM_PREFIX          = "Item"
	ITEM_EMPTY           = "Empty"
)

var (
	inventoryStartPrefix = []byte(INVENTORY_START + " ")
	itemPrefix           = []byte(ITEM_PREFIX + " ")
)

// maxListCapacityHint caps the preallocation taken from the (untrusted) list size field
const maxListCapacityHint = 1024

func parseMetadata(c *cursor, mapblock *types.MapBlock) error {
	version, err := c.u8()
	if err != nil {
		return err
	}

	if version == 0 {
		// no metadata
		return nil
	}

	count, err := c.u16()
	if err != nil {
		return err
	}

	for i := 0; i < int(count); i++ {
		pos, err := c.u16()
		if err != nil {
			return err
		}
		position := int(pos)

		valuecount, err := c.u32()
		if err != nil {
			return err
		}
		// every value takes at least 6 bytes (u16 key length + u32 value length);
		// reject impossible counts before using them as an allocation hint
		if uint64(valuecount)*6 > uint64(c.remaining()) {
			return fmt.Errorf("%w: metadata value count %d at offset %d", ErrTruncated, valuecount, c.off)
		}

		fields := make(map[string]string, valuecount)
		mapblock.Fields[position] = fields

		for j := 0; j < int(valuecount); j++ {
			keyLength, err := c.u16()
			if err != nil {
				return err
			}
			key, err := c.take(int(keyLength))
			if err != nil {
				return err
			}

			valueLength, err := c.u32()
			if err != nil {
				return err
			}
			if uint64(valueLength) > uint64(c.remaining()) {
				return fmt.Errorf("%w: metadata value length %d at offset %d, have %d", ErrTruncated, valueLength, c.off, c.remaining())
			}
			value, err := c.take(int(valueLength))
			if err != nil {
				return err
			}

			fields[string(key)] = string(value)

			if version >= 2 {
				// private flag, doesn't exist in version 1
				if err = c.skip(1); err != nil {
					return err
				}
			}
		}

		if err = parseInventory(c, mapblock, position); err != nil {
			return err
		}
	}

	return nil
}

// parseInventory parses the text-serialized inventory up to and including "EndInventory"
func parseInventory(c *cursor, mapblock *types.MapBlock, position int) error {
	var inv map[string][]string
	var listName string
	var list []string
	inList := false

	flush := func() {
		if inList {
			inv[listName] = list
		}
	}

	for {
		line, err := c.line()
		if err != nil {
			return fmt.Errorf("inventory not terminated: %w", err)
		}
		line = bytes.TrimSuffix(line, []byte{'\r'})

		switch {
		case string(line) == INVENTORY_TERMINATOR:
			flush()
			return nil

		case bytes.HasPrefix(line, inventoryStartPrefix):
			flush()
			parts := bytes.Fields(line)
			if len(parts) < 2 {
				return fmt.Errorf("malformed inventory list header: %q", line)
			}
			if inv == nil {
				inv = mapblock.Inventory[position]
				if inv == nil {
					inv = map[string][]string{}
					mapblock.Inventory[position] = inv
				}
			}
			capHint := 0
			if len(parts) >= 3 {
				if n, err := strconv.Atoi(string(parts[2])); err == nil && n > 0 {
					capHint = min(n, maxListCapacityHint)
				}
			}
			listName = string(parts[1])
			list = make([]string, 0, capHint)
			inList = true
			inv[listName] = list

		case string(line) == INVENTORY_END:
			flush()
			inList = false
			list = nil

		case inList:
			if bytes.HasPrefix(line, itemPrefix) {
				list = append(list, string(line[len(itemPrefix):]))
			} else if string(line) == ITEM_EMPTY {
				list = append(list, "")
			}
			// other lines (e.g. "Width 0") are ignored

		default:
			return fmt.Errorf("malformed inventory: %q", line)
		}
	}
}
