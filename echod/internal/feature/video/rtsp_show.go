//go:build !spot && !dot

package video

// rtspFenced is whether this device's kernel can keep a decoder reading RTSP off the device's own
// network (rtspFence): the Show's can.
const rtspFenced = true
