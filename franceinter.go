package main

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/antchfx/htmlquery"
	"github.com/gorilla/feeds"
	"golang.org/x/net/html"
)

type FranceInterCrawler struct {
	RadioFranceCrawler
}

func (c *FranceInterCrawler) Name() string {
	return "franceinter"
}

func (c *FranceInterCrawler) Fetch(name string, cachedRss *feeds.RssFeedXml) (*feeds.Feed, error) {
	url := fmt.Sprintf("https://www.franceinter.fr/emissions/%s", name)
	content, _, err := c.cache.Get(url)
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(bytes.NewBuffer(content))
	if err != nil {
		return nil, err
	}

	rssLink := htmlquery.Find(doc, "//a[@class='podcast-button rss']")
	if len(rssLink) == 0 {
		return nil, errors.New("failed to find RSS feed")
	}

	var rssFeedLink string
	for _, attr := range rssLink[0].Attr {
		if attr.Key == "href" {
			rssFeedLink = attr.Val
		}
	}

	if rssFeedLink == "" {
		return nil, errors.New("failed to find RSS feed")
	}

	return c.getFeed(url, doc, rssFeedLink, cachedRss)
}

func newFranceInterCrawler(cache *HTTPCache) *FranceInterCrawler {
	return &FranceInterCrawler{
		RadioFranceCrawler: RadioFranceCrawler{
			cache: cache,
			url:   "https://www.franceinter.fr",
		},
	}
}
