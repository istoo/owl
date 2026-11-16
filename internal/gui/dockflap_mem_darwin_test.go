//go:build darwin && cgo && !nogui

package gui

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// TestDockFlapHoldsOneImage holds a flap's frames to one image. NSDockTile
// keeps every image it is shown: an image a frame stayed in the app for
// good (29 MB a click, seen on a live owl), while the same image shown
// again is kept once. The test measures the process's footprint across a
// flap: one image is a couple of MB, an image a frame is some 29 MB more.
func TestDockFlapHoldsOneImage(t *testing.T) {
	out, err := runAppKit(t, 60*time.Second, []string{"OWL_TEST_DOCK_MEM=1"})
	if err != nil || !strings.Contains(string(out), "dock mem: ok") {
		t.Fatalf("dock flap memory: %v\n%s", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "dock mem:") {
			t.Log(line)
		}
	}
}

func init() {
	if os.Getenv("OWL_TEST_DOCK_MEM") != "1" {
		return
	}
	app := application.New(application.Options{
		Name: "owl dock flap memory regression",
		Mac:  application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory},
		Assets: application.AssetOptions{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<p>owl</p>`)
		})},
	})
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			dockPlainForTest.Store(1) // the machine's Dock look is not what this measures
			if !dockReady("") {
				fmt.Println("dock mem: the cut icon couldn't be made")
				app.Quit()
				os.Exit(1)
			}
			flap := func() uint64 {
				dockFlap()
				// the frames the flap put on the main queue, drawn and shown
				application.InvokeSync(func() {})
				time.Sleep(200 * time.Millisecond)
				return dockFootprint()
			}
			base := dockFootprint()
			first := flap()
			second := flap()
			if shown := dockFramesShown(); shown != 58 {
				fmt.Printf("dock mem: %d frames shown, want 58\n", shown)
				app.Quit()
				os.Exit(1)
			}
			if grew := first - base; grew > 12<<20 {
				fmt.Printf("dock mem: a flap left %d MB, want the frames to share one image\n", grew>>20)
				app.Quit()
				os.Exit(1)
			}
			// and the next click no more: an image kept a flap is kept once
			if grew := second - first; grew > 4<<20 {
				fmt.Printf("dock mem: a second flap left %d MB more\n", grew>>20)
				app.Quit()
				os.Exit(1)
			}
			fmt.Printf("dock mem: ok, a flap left %d KB, the next %d KB\n", (first-base)>>10, (second-first)>>10)
			app.Quit()
		}()
	})
	if err := app.Run(); err != nil {
		panic(err)
	}
	os.Exit(0)
}
