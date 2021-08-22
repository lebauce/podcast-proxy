package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"
	"path"

	"github.com/gin-gonic/gin"
)

func main() {
	var listenPort int
	var listenAddr string

	flag.IntVar(&listenPort, "port", 8080, "port to listen on")
	flag.StringVar(&listenAddr, "addr", "0.0.0.0", "listen IP to bind")
	flag.Parse()

	store, err := NewStore(path.Join(os.Getenv("HOME"), ".cache", "podcast-proxy"))
	if err != nil {
		panic(err)
	}

	registerCrawlers(store.httpCache)

	r := gin.Default()

	getEmission := func(c *gin.Context, crawler, name string) (string, error) {
		emission, err := store.Get(crawler, name)
		if err != nil {
			return "", err
		}

		html := `<html><body><ul>`
		for _, item := range emission.feed.Items {
			html += "<p>"
			html += fmt.Sprintf("<b>%s</b><br>", item.Title)
			html += fmt.Sprintf("<a href='%s'>Download</a>", item.Enclosure.Url)
			html += "</p>"
			html += "<hr />"
		}
		html += `</ul></body></html>`
		return html, nil
	}

	r.GET("/emissions", func(c *gin.Context) {
		emissions, err := store.List()
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("error: %s", err))
			return
		}

		html := `<html><body><ul>`
		for _, emission := range emissions {
			html += "<p>"
			if image := emission.feed.Image; image != nil {
				html += fmt.Sprintf("<img src=\"%s\" width=50 style=\"vertical-align: middle\"/>", image.Url)
			}
			html += fmt.Sprintf("<b>%s</b><br>", emission.feed.Title)
			html += fmt.Sprintf("<div><a href='/emissions/%s/%s/rss'>RSS</a>", emission.crawler.Name(), emission.name)
			html += fmt.Sprintf("&nbsp;<a href='/emissions/%s/%s'>Episodes</a></div>", emission.crawler.Name(), emission.name)
			html += "</p><hr />"
		}
		html += `</ul></body></html>`

		c.Data(200, "text/html; charset=utf-8", []byte(html))
	})
	r.GET("/emissions/:crawler/:name", func(c *gin.Context) {
		crawler := c.Param("crawler")
		name := c.Param("name")

		html, err := getEmission(c, crawler, name)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("error: %s", err))
			return
		}

		c.Data(200, "text/html; charset=utf-8", []byte(html))
	})
	r.GET("/emissions/:crawler/:name/rss", func(c *gin.Context) {
		crawler := c.Param("crawler")
		name := c.Param("name")

		rss, err := store.RSS(crawler, name)
		if err != nil {
			c.String(http.StatusInternalServerError, fmt.Sprintf("error: %s", err))
			return
		}
		c.Data(200, "application/xml; charset=utf-8", []byte(rss))
	})

	r.Run(fmt.Sprintf("%s:%d", listenAddr, listenPort))
}
