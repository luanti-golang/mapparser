package mapparser

import (
	"github.com/luanti-golang/types"
)

func parseBlockMapping(c *cursor, mapblock *types.MapBlock) error {
	numMappings, err := c.u16()
	if err != nil {
		return err
	}

	for i := 0; i < int(numMappings); i++ {
		nodeId, err := c.u16()
		if err != nil {
			return err
		}

		nameLen, err := c.u16()
		if err != nil {
			return err
		}

		name, err := c.take(int(nameLen))
		if err != nil {
			return err
		}

		blockName := string(name)
		mapblock.BlockMapping[int(nodeId)] = blockName

		if numMappings == 1 && blockName == "air" {
			// mark as air-only
			mapblock.AirOnly = true
		}
	}
	return nil
}
