package home

import (
	"bytes"
	"image"
	"image/jpeg"
	"path/filepath"
	"testing"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

func jpegOf(t *testing.T, w, h int) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A photo is kept, becomes the slideshow's source on a device with no Home Assistant, and reads back;
// what is not a picture, or is a camera's full size, is not kept.
func TestPhotosKeptOnTheDevice(t *testing.T) {
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	old := PhotosDir
	PhotosDir = filepath.Join(t.TempDir(), "photos")
	defer func() { PhotosDir = old }()

	if err := AddLocalPhoto(jpegOf(t, 1600, 1200)); err != nil {
		t.Fatal(err)
	}
	if n := LocalPhotoCount(); n != 1 {
		t.Fatalf("%d photos kept", n)
	}
	if src := config.Get().Home.Slideshow.Source; src != LocalPhotos {
		t.Errorf("the slideshow's source is %q, want the device's photos", src)
	}
	list := localPhotos()
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	if b, err := localPhoto(list[0].ID); err != nil || len(b) == 0 {
		t.Errorf("reading it back: %v", err)
	}
	if _, err := localPhoto(localPhotoPrefix + "../../state.json"); err == nil {
		t.Error("a photo id reached outside the folder")
	}

	if err := AddLocalPhoto([]byte("not a picture")); err == nil {
		t.Error("text was kept as a photo")
	}
	if err := AddLocalPhoto(jpegOf(t, 5000, 100)); err == nil {
		t.Error("a picture bigger than the device decodes was kept")
	}
	if err := RemoveLocalPhotos(); err != nil || LocalPhotoCount() != 0 {
		t.Errorf("removing: %v, %d left", err, LocalPhotoCount())
	}
}
