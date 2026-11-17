package main

import (
	"embed"
	"io/fs"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"nudge/desktop/bridge"
)

//go:embed all:dist
var frontend embed.FS

//go:embed org.wails.nudge.svg
var icon []byte

func main() {
	port := os.Getenv("NUDGE_PORT")
	if port == "" {
		port = "8788"
	}
	client, err := bridge.New(port)
	if err != nil {
		log.Fatal(err)
	}
	assets, err := fs.Sub(frontend, "dist")
	if err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name:        "Nudge",
		Description: "Gentle reminders for intentional days",
		Icon:        icon,
		Linux:       application.LinuxOptions{ApplicationID: "org.wails.nudge"},
		Services:    []application.Service{application.NewService(client)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(assets)},
	})
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "Nudge", Width: 1060, Height: 760, MinWidth: 360, MinHeight: 500, URL: "/",
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
