package mapparser

// parseStaticObjects skips over the static object list
func parseStaticObjects(c *cursor) error {
	// static objects version
	if err := c.skip(1); err != nil {
		return err
	}

	count, err := c.u16()
	if err != nil {
		return err
	}

	for i := 0; i < int(count); i++ {
		// u8 type, 3x s32 position
		if err = c.skip(13); err != nil {
			return err
		}
		dataSize, err := c.u16()
		if err != nil {
			return err
		}
		if err = c.skip(int(dataSize)); err != nil {
			return err
		}
	}
	return nil
}
