// Command short-report runs a local web app that shows today's date and
// an LLM-aggregated news briefing, filtered by user-chosen tags, whose
// length is controlled by a word-count slider.
package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"time"

	"shortreport/internal/auth"
	"shortreport/internal/config"
	"shortreport/internal/news"
	"shortreport/internal/server"
)

//go:embed all:web/dist
var webFS embed.FS

func main() {
	addr := flag.String("addr", "127.0.0.1:8787", "address to listen on")
	flag.Parse()

	users, err := config.NewUserStore()
	if err != nil {
		log.Fatalf("setting up account storage: %v", err)
	}
	sessions := auth.NewManager()

	assets, err := fs.Sub(webFS, "web/dist")
	if err != nil {
		log.Fatalf("loading embedded assets: %v", err)
	}

	cache := news.NewCache(20 * time.Minute)
	srv := server.New(users, sessions, cache, assets)

	log.Printf("Short Report is running at http://%s", *addr)
	if err := http.ListenAndServe(*addr, srv.Routes()); err != nil {
		log.Fatal(err)
	}
}
