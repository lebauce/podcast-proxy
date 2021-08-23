package main

import (
	"bytes"
	"fmt"
	"regexp"

	"github.com/gorilla/feeds"
	"golang.org/x/net/html"
)

type FranceCultureCrawler struct {
	RadioFranceCrawler
}

func (c *FranceCultureCrawler) Name() string {
	return "franceculture"
}

func (c *FranceCultureCrawler) Fetch(name string, cachedRss *feeds.RssFeedXml) (*feeds.Feed, error) {
	url := fmt.Sprintf("https://www.franceculture.fr/emissions/%s", name)
	content, _, err := c.cache.Get(url)
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(bytes.NewBuffer(content))
	if err != nil {
		return nil, err
	}

	re := regexp.MustCompile("https?://radiofrance-podcast.net/podcast09/rss_[0-9]+.xml")
	rssFeedLink := re.FindString(string(content))

	return c.getFeed(url, doc, rssFeedLink, cachedRss)
}

func newFranceCultureCrawler(cache *HTTPCache) *FranceCultureCrawler {
	return &FranceCultureCrawler{
		RadioFranceCrawler: RadioFranceCrawler{
			cache:                 cache,
			buttonIsFigureSibling: true,
			url:                   "https://www.franceculture.fr",
		},
	}
}
