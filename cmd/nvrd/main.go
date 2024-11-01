package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/rs/zerolog"
	"github.com/sigcn/nvr/account"
	"github.com/sigcn/nvr/camera"
	"github.com/sigcn/nvr/cmd/nvrd/static"
	"github.com/sigcn/nvr/recorder"
	ffmpeg "github.com/u2takey/ffmpeg-go"
	"github.com/use-go/onvif/sdk"
)

var (
	storePath string
	listen    string
	logLevel  int
)

func main() {
	flag.StringVar(&storePath, "store", "/var/lib/nvrd", "store path")
	flag.StringVar(&listen, "listen", ":2998", "listen addr")
	flag.IntVar(&logLevel, "loglevel", 0, "logging level")
	flag.BoolFunc("v", "print version", printVersion)
	flag.Parse()

	sdk.Logger = sdk.Logger.Level(zerolog.InfoLevel)
	ffmpeg.LogCompiledCommand = false
	slog.SetLogLoggerLevel(slog.Level(logLevel))
	if logLevel <= -2 {
		sdk.Logger = sdk.Logger.Level(zerolog.DebugLevel)
		ffmpeg.LogCompiledCommand = true
	}

	if err := os.MkdirAll(storePath, 0600); err != nil && !os.IsExist(err) {
		slog.Error("Creating store", "path", storePath, "err", err)
	}

	httpserver := server{
		cameraStore:     &camera.FileStore{Path: filepath.Join(storePath, "cameras")},
		recorderManager: &recorder.Manager{},
		apiKeyStore:     &account.SimpleApiKeyStore{Path: storePath},
	}

	go httpserver.watchProcessSignal()

	mux := http.NewServeMux()
	mux.HandleFunc("GET    /media/{camera_id}/live.ts", httpserver.handleMediaMPEGTS)
	mux.HandleFunc("POST   /v1/api/keys", httpserver.handleCreateApiKey)
	mux.HandleFunc("DELETE /v1/api/keys", httpserver.handleDeleteApiKey)
	mux.HandleFunc("POST   /v1/api/cameras", httpserver.handleCreateCamera)
	mux.HandleFunc("GET    /v1/api/cameras", httpserver.handleListCameras)
	mux.HandleFunc("DELETE /v1/api/cameras/{camera_id}", httpserver.handleDeleteCamera)
	mux.HandleFunc("PATCH  /v1/api/cameras/{camera_id}/remark", httpserver.handleUpdateCameraRemark)
	mux.HandleFunc("POST   /v1/api/cameras/reload", httpserver.handleReloadCameras)
	mux.HandleFunc("GET    /v1/api/stat", handleStat)
	mux.HandleFunc("GET    /", handleStaticFiles)

	if err := http.ListenAndServe(listen, corsMiddleware(httpserver.middlewareApiKey(mux))); err != nil {
		panic(err)
	}
}

func handleStaticFiles(w http.ResponseWriter, r *http.Request) {
	f, err := static.FS.Open(strings.TrimPrefix(r.URL.Path, "/"))
	if err != nil {
		http.ServeFileFS(w, r, static.FS, "index.html")
		return
	}
	f.Close()
	http.ServeFileFS(w, r, static.FS, strings.TrimPrefix(r.URL.Path, "/"))
}

func printVersion(s string) error {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return errors.ErrUnsupported
	}
	fmt.Println(info.GoVersion)
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			fmt.Println("commit\t", s.Value)
			continue
		}
		if s.Key == "vcs.time" {
			fmt.Println("time\t", s.Value)
		}
	}
	os.Exit(0)
	return nil
}
