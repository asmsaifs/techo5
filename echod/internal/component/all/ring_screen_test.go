//go:build !dot

package all

// ringOnly is what only a device with an LED ring puts up (led.HasRing): the Echo Show and the Echo Spot
// have none.
var ringOnly = []string{"failure_effect", "ring", "ring_muted", "room_reaction", "segment_1", "segment_2", "segment_3", "segment_4", "segment_5", "segment_6", "segment_7", "segment_8", "segment_9", "segment_10", "segment_11", "segment_12"}
