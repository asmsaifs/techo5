//go:build spot

package video

// rtspFenced is whether this device's kernel can keep a decoder reading RTSP off the device's own
// network (rtspFence): the Spot's has no owner match, so a camera is read through Home Assistant's HLS
// instead, and nothing offers RTSP on it (CanRTSP).
const rtspFenced = false
