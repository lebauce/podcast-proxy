package main

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	"github.com/gorilla/feeds"
	"golang.org/x/net/html"
)

type RadioFranceCrawler struct {
	cache                 *HTTPCache
	buttonIsFigureSibling bool
	url                   string
}

func (c *RadioFranceCrawler) getFeed(url string, doc *html.Node, rssFeedLink string, cachedRss *feeds.RssFeedXml) (*feeds.Feed, error) {
	rssFeed, err := parseRSS(c.cache, rssFeedLink)
	if err != nil {
		return nil, err
	}

	if len(rssFeed.Channel.Items) == 0 {
		return nil, errors.New("empty RSS feed")
	}

	feed := &feeds.Feed{
		Title:       rssFeed.Channel.Title,
		Description: rssFeed.Channel.Description,
		Copyright:   rssFeed.Channel.Copyright,
		Link: &feeds.Link{
			Href: rssFeed.Channel.Link,
		},
		Image: &feeds.Image{
			Url:    rssFeed.Channel.Image.Url,
			Title:  rssFeed.Channel.Image.Title,
			Link:   rssFeed.Channel.Image.Link,
			Width:  rssFeed.Channel.Image.Width,
			Height: rssFeed.Channel.Image.Height,
		},
	}

	if lastBuildDate, err := time.Parse(time.RFC822, rssFeed.Channel.LastBuildDate); err != nil {
		feed.Updated = lastBuildDate
	}

	cachedItems := make(map[string]*feeds.RssItem)
	if cachedRss != nil {
		for _, cachedItem := range cachedRss.Channel.Items {
			cachedItems[cachedItem.Link] = cachedItem
		}

		defer func() {
			for _, previousEntry := range cachedRss.Channel.Items {
				feed.Items = append(feed.Items, itemFromRss(previousEntry))
			}
		}()
	}

	for page := 1; page <= len(htmlquery.Find(doc, "//li[@class='pager-item']/a"))+1; page++ {
		entries, err := c.parsePage(fmt.Sprintf("%s?p=%d", url, page), rssFeed.Channel.Items[0])
		if err != nil {
			return nil, err
		}

		for _, entry := range entries {
			if _, found := cachedItems[entry.Link.Href]; found {
				return feed, nil
			}

			// Get item description
			content, _, err := c.cache.Get(entry.Link.Href)
			if err != nil {
				return nil, err
			}

			doc, err := html.Parse(bytes.NewBuffer(content))
			if err != nil {
				return nil, err
			}

			description := htmlquery.Find(doc, "//article//p")
			if len(description) > 0 {
				entry.Description = strings.TrimSpace(description[0].FirstChild.Data)
			} else {
				log.Printf("failed to find article for %s\n", entry.Link.Href)
			}

			// Get media length
			_, header, err := c.cache.Head(entry.Enclosure.Url)
			if err != nil {
				return nil, err
			}
			entry.Enclosure.Length = header.Get("Content-Length")

			feed.Items = append(feed.Items, entry)
		}
	}

	return feed, nil
}

func (c *RadioFranceCrawler) parseFigure(node *html.Node, templateItem *feeds.RssItem) (*feeds.Item, error) {
	var button *html.Node
	if c.buttonIsFigureSibling {
		node = node.Parent.Parent
	}

	buttons := htmlquery.Find(node, "//button")

	for _, button = range buttons {
		if getAttribute(button, "data-broadcast-type") == "replay" &&
			getAttribute(button, "data-is-aod") == "1" {
			break
		}
	}
	if button == nil {
		return nil, errors.New("failed to find button")
	}

	title := getAttribute(button, "data-diffusion-title")
	contentURL := getAttribute(button, "data-url")
	if !strings.HasSuffix(contentURL, ".mp3") {
		return nil, fmt.Errorf("unsupported media: %s", contentURL)
	}
	startTimeEpoch, _ := strconv.Atoi(getAttribute(button, "data-start-time"))
	startTime := time.Unix(int64(startTimeEpoch), 0)

	link := htmlquery.FindOne(node, "//a[1]")
	if link == nil {
		return nil, errors.New("failed to find link")
	}
	href := getAttribute(link, "href")

	if !strings.HasPrefix(href, "https://") {
		href = c.url + href
	}

	return &feeds.Item{
		Title: title,
		Link: &feeds.Link{
			Href: href,
		},
		Author: &feeds.Author{
			Name: templateItem.Author,
		},
		Enclosure: &feeds.Enclosure{
			Url:  contentURL,
			Type: "audio/mpeg",
		},
		Created: startTime,
	}, nil
}

func (c *RadioFranceCrawler) parsePage(url string, templateItem *feeds.RssItem) ([]*feeds.Item, error) {
	content, _, err := c.cache.Get(url)
	if err != nil {
		return nil, err
	}

	doc, err := html.Parse(bytes.NewBuffer(content))
	if err != nil {
		return nil, err
	}

	var items []*feeds.Item
	figures := htmlquery.Find(doc, "//figure")
	for _, figure := range figures {
		item, err := c.parseFigure(figure, templateItem)
		if err != nil {
			fmt.Printf("failed to parse figure: %s\n", err)
			continue
		}
		items = append(items, item)
	}

	return items, nil
}
