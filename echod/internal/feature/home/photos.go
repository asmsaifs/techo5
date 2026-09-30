package home

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
	"github.com/HuskerMinion/techo5/echod/internal/layout"
	"github.com/HuskerMinion/techo5/echod/internal/lib/hass"
)

// Photos kept on the device, for the slideshow of a device with no Home Assistant media library to
// show: uploaded on the setup page, shrunk to the screen by the browser first, and shown by the
// slideshow as its source LocalPhotos.

// LocalPhotos is the slideshow source that is the device's own photos.
const LocalPhotos = "device:photos"

// localPhotoPrefix begins a photo's id in the slideshow's list.
const localPhotoPrefix = "device:"

const (
	// MaxLocalPhotos is how many the device keeps: at a few hundred kilobytes each, a few hundred
	// megabytes of a userdata that has gigabytes.
	MaxLocalPhotos = 1000

	// maxPhotoSide refuses a picture that was not shrunk: the device decodes each one it shows, and a
	// camera's full size is more than its memory wants.
	maxPhotoSide = 4096
)

// PhotosDir is where they are kept.
var PhotosDir = filepath.Join(layout.StateDir, "photos")

// LocalPhotoCount is how many are kept.
func LocalPhotoCount() int { return len(localPhotoNames()) }

func localPhotoNames() []string {
	es, err := os.ReadDir(PhotosDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range es {
		n := strings.ToLower(e.Name())
		if !e.IsDir() && (strings.HasSuffix(n, ".jpg") || strings.HasSuffix(n, ".png")) {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// AddLocalPhoto keeps one picture, a JPEG or a PNG, and makes the device's photos the slideshow's
// source where it has none of Home Assistant's to show.
func AddLocalPhoto(b []byte) error {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return errors.New("that is not a picture this device can show (JPEG or PNG)")
	}
	if format != "jpeg" && format != "png" {
		return fmt.Errorf("%s pictures are not shown here: JPEG or PNG", format)
	}
	if cfg.Width > maxPhotoSide || cfg.Height > maxPhotoSide {
		return fmt.Errorf("the picture is %dx%d: at most %d on a side", cfg.Width, cfg.Height, maxPhotoSide)
	}
	if LocalPhotoCount() >= MaxLocalPhotos {
		return fmt.Errorf("the device already keeps %d photos, the most it holds", MaxLocalPhotos)
	}
	if err := os.MkdirAll(PhotosDir, 0o755); err != nil {
		return err
	}
	var id [8]byte
	_, _ = rand.Read(id[:])
	ext := ".jpg"
	if format == "png" {
		ext = ".png"
	}
	name := filepath.Join(PhotosDir, hex.EncodeToString(id[:])+ext)
	if err := os.WriteFile(name+".new", b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(name+".new", name); err != nil {
		return err
	}
	h := config.Get().Home.Slideshow
	if h.Source == "" || !hass.Get().Ready() {
		if h.Source != LocalPhotos {
			Get().SetSlideshowSource(LocalPhotos)
		}
	}
	Get().forgetSlideshowList()
	return nil
}

// RemoveLocalPhotos deletes every photo kept on the device.
func RemoveLocalPhotos() error {
	for _, n := range localPhotoNames() {
		if err := os.Remove(filepath.Join(PhotosDir, n)); err != nil {
			return err
		}
	}
	slog.Info("home: the device's own photos were removed")
	Get().forgetSlideshowList()
	return nil
}

// localPhotos is the slideshow's list of them.
func localPhotos() []hass.Media {
	var out []hass.Media
	for _, n := range localPhotoNames() {
		out = append(out, hass.Media{ID: localPhotoPrefix + n, Title: n, Kind: "image/jpeg", CanPlay: true})
	}
	return out
}

// localPhoto reads one by its slideshow id.
func localPhoto(id string) ([]byte, error) {
	name := filepath.Base(strings.TrimPrefix(id, localPhotoPrefix)) // no way out of the folder
	return os.ReadFile(filepath.Join(PhotosDir, name))
}

// forgetSlideshowList has the next photo gathered afresh, so what was just added or removed shows.
func (f *Feature) forgetSlideshowList() {
	f.mu.Lock()
	f.slideshow.children, f.slideshow.fails, f.slideshow.waitUntil = nil, 0, time.Time{}
	f.slideshow.trouble = ""
	f.mu.Unlock()
	f.Changed.Emit(struct{}{})
}
