package unbound

// Reload makes unbound re-read its config and rebuild in-memory state. The
// tool's only runtime write: the fragment file is the source of truth, and a
// reload projects it onto the running daemon.
func (c *Client) Reload() error {
	_, err := c.run("", "reload")
	return err
}
