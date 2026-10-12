//go:build dot

package doorbell

// showCamera does nothing on a Dot, which has no screen: the chime is the whole of it there.
func showCamera(string) {}
