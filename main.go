// Command noted is an offline logbook for a technical electronics test
// environment: record what you changed and why, then find it again months later.
//
// Everything lives on this machine. There is deliberately no network code, no
// sync and no accounts: the notes describe hardware under test and belong to the
// bench they were written at, and an offline tool has nothing to configure, log
// into, or lose access to when the lab network is down.
package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// The built frontend is embedded into the executable, which is what makes the
// Windows build a single file to copy onto a lab PC.
//
//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "noted",
		Description: "An offline logbook for a technical electronics test environment",
		Services: []application.Service{
			// NoteService opens the database in its ServiceStartup hook and
			// closes it in ServiceShutdown, so the lifetime of the store matches
			// the lifetime of the app without main having to manage it.
			application.NewService(NewNoteService()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "noted",
		Width:  1100,
		Height: 720,
		// Below roughly this size the list and editor stop being usable side by side.
		MinWidth:  720,
		MinHeight: 480,
		// Matches --bg in frontend/public/style.css, so there is no white flash
		// before the stylesheet loads.
		BackgroundColour: application.NewRGB(24, 24, 27),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
