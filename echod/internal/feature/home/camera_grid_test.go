package home

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/config"
)

// gridHouse is a device with the cameras named, whose snapshots are a picture of 1280 by 720, or an
// error for the ones in failing.
func gridHouse(t *testing.T, cams []config.Camera, failing ...string) (*Feature, *atomic.Int32) {
	t.Helper()
	config.Use(filepath.Join(t.TempDir(), "state.json"))
	if err := config.Set().Home().Cameras(cams); err != nil {
		t.Fatal(err)
	}
	var snap bytes.Buffer
	if err := jpeg.Encode(&snap, image.NewRGBA(image.Rect(0, 0, 1280, 720)), nil); err != nil {
		t.Fatal(err)
	}
	var fetched atomic.Int32
	var mu sync.Mutex
	wasFetch, wasEvery := fetchCamera, gridEvery
	fetchCamera = func(_ context.Context, entity string, _ int) ([]byte, error) {
		fetched.Add(1)
		mu.Lock()
		defer mu.Unlock()
		for _, f := range failing {
			if f == entity {
				return nil, errors.New("no picture")
			}
		}
		return snap.Bytes(), nil
	}
	gridEvery = 10 * time.Millisecond
	t.Cleanup(func() { fetchCamera, gridEvery = wasFetch, wasEvery })
	return newFeature(t), &fetched
}

// settled waits for every cell of the grid up to have a picture or a reason it has none.
func settled(t *testing.T, f *Feature) CameraView {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		v, up := f.Camera()
		done := up && len(v.Grid) > 0
		for _, c := range v.Grid {
			done = done && (c.Frame != nil || c.Error != "")
		}
		if done {
			return v
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the grid's cells never filled")
	return CameraView{}
}

func TestTheGridShowsTheCamerasListed(t *testing.T) {
	f, _ := gridHouse(t, []config.Camera{
		{Entity: "camera.front_door", Name: "Front door"},
		{Entity: "camera.deck", Name: "Deck"},
		{Entity: "camera.garage", Name: "Garage"},
	}, "camera.garage")
	if err := f.ShowGrid(nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	v := settled(t, f)
	if v.Entity != GridEntity || len(v.Grid) != 3 {
		t.Fatalf("up: %q with %d cells", v.Entity, len(v.Grid))
	}
	if v.Grid[0].Name != "Front door" || v.Grid[1].Entity != "camera.deck" {
		t.Errorf("cells %+v", v.Grid)
	}
	cols, rows := GridLayout(3)
	for _, c := range v.Grid[:2] {
		if c.Frame == nil {
			t.Fatalf("%s has no picture", c.Entity)
		}
		if b := c.Frame.Bounds(); b.Dx() > cameraFrameW/cols || b.Dy() > cameraFrameH/rows {
			t.Errorf("%s's picture is %v, more than a cell", c.Entity, b)
		}
	}
	if v.Grid[2].Frame != nil || v.Grid[2].Error == "" {
		t.Errorf("a camera with no picture says %+v", v.Grid[2])
	}
}

func TestTheGridShowsAtMostSix(t *testing.T) {
	var cams []config.Camera
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		cams = append(cams, config.Camera{Entity: "camera." + n, Name: n})
	}
	f, _ := gridHouse(t, cams)
	if err := f.ShowGrid(nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.Camera(); len(v.Grid) != MaxGridCells {
		t.Errorf("%d cells, want %d", len(v.Grid), MaxGridCells)
	}
}

func TestTheGridShowsTheCamerasAskedFor(t *testing.T) {
	f, _ := gridHouse(t, []config.Camera{{Entity: "camera.front_door", Name: "Front door"}})
	if err := f.ShowGrid([]string{"camera.porch", "camera.front_door", "camera.porch", LocalCamera}, time.Minute); err != nil {
		t.Fatal(err)
	}
	v, _ := f.Camera()
	if len(v.Grid) != 2 || v.Grid[0].Name != "porch" || v.Grid[1].Name != "Front door" {
		t.Errorf("cells %+v; want the porch, by its id, then the front door, once each and without the device's own", v.Grid)
	}
}

func TestNoCamerasNoGrid(t *testing.T) {
	f, _ := gridHouse(t, nil)
	if err := f.ShowGrid(nil, time.Minute); err == nil {
		t.Error("a grid of nothing went up")
	}
	if _, up := f.Camera(); up {
		t.Error("a view is up")
	}
}

// Taken down, the grid stops fetching.
func TestAGridTakenDownStopsFetching(t *testing.T) {
	f, fetched := gridHouse(t, []config.Camera{{Entity: "camera.a", Name: "A"}, {Entity: "camera.b", Name: "B"}})
	if err := f.ShowGrid(nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	settled(t, f)
	f.HideCamera()
	f.loops.Wait()
	n := fetched.Load()
	time.Sleep(50 * time.Millisecond)
	if fetched.Load() != n {
		t.Error("the grid went on fetching after it was taken down")
	}
}

// A camera tapped out of the grid replaces it.
func TestACameraFromTheGridReplacesIt(t *testing.T) {
	f, _ := gridHouse(t, []config.Camera{{Entity: "camera.a", Name: "A"}, {Entity: "camera.b", Name: "B"}})
	if err := f.ShowGrid(nil, time.Minute); err != nil {
		t.Fatal(err)
	}
	f.ShowCamera("camera.b", time.Minute)
	v, up := f.Camera()
	if !up || v.Entity != "camera.b" || len(v.Grid) != 0 {
		t.Errorf("after a camera from the grid: %q with %d cells", v.Entity, len(v.Grid))
	}
}
