package home

import "testing"

// Only a camera's own entity ID goes into the path of a request made with the device's token.
func TestCameraEntity(t *testing.T) {
	for _, e := range []string{"camera.front_door", "camera.deck2"} {
		if !CameraEntity(e) {
			t.Errorf("%q refused", e)
		}
	}
	for _, e := range []string{"", "camera.", "camera.x/../../api/states", "camera.x?y=1", "camera.Front", "image.x", "camera.x y"} {
		if CameraEntity(e) {
			t.Errorf("%q let through", e)
		}
	}
	if _, _, err := pictureSource("camera.x/../../api/states"); err == nil {
		t.Error("a picture source with a path after the camera was taken")
	}
}
