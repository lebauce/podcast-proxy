package main

import (
	"github.com/gorilla/feeds"
)

var (
	crawlers       map[string]Crawler
	defaultCrawler = "franceinter"
)

type Crawler interface {
	Name() string
	Fetch(name string, cachedRss *feeds.RssFeedXml) (*feeds.Feed, error)
}

func getCrawler(name string) Crawler {
	return crawlers[name]
}

func registerCrawlers(cache *HTTPCache) {
	crawlers = map[string]Crawler{
		"franceinter":   newFranceInterCrawler(cache),
		"franceculture": newFranceCultureCrawler(cache),
	}
}
