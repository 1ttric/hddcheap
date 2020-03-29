package main

import (
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"github.com/tebeka/selenium"
	"github.com/tebeka/selenium/chrome"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

func init() {
	log.SetLevel(log.TraceLevel)
}

var (
	// Used to extract an item's size from its title
	reCapacity = regexp.MustCompile("(?i)(\\d+\\.?\\d*?).?TB")
)

// Represents a scraped Amazon storage item
type Item struct {
	ASIN       string  `json:"asin"`
	URL        string  `json:"url"`
	Name       string  `json:"name"`
	Price      float32 `json:"price"`
	Capacity   float32 `json:"capacity"`
	Efficiency float32 `json:"efficiency"`
}

type ItemStore struct {
	fetcher   ItemFetcher
	items     []Item
	itemChans []chan []Item

	stop    chan struct{}
	stopped chan struct{}
}

// Refreshes the items in the store
func (s *ItemStore) refresh() {
	log.Debugf("item store refreshing")
	var sortedItems []Item
	for page := 1; page < 4; page++ {
		itemChunk, err := s.fetcher.FetchItems(page)
		if err != nil {
			log.Warnf("skipping results page %d: %s", page, err.Error())
			continue
		}
		sortedItems = append(sortedItems, itemChunk...)
	}
	sort.Slice(sortedItems, func(i, j int) bool {
		return sortedItems[i].Efficiency < sortedItems[j].Efficiency
	})
	log.Infof("item store refreshed with %d results", len(sortedItems))

	s.items = sortedItems
	// Sends the items to each listening item channel
	for _, itemChan := range s.itemChans {
		itemChan <- s.items
	}
}

// Fetches current items in the store
func (s *ItemStore) Items() []Item {
	return s.items
}

// Returns a channel that will output the item list as it is updated
func (s *ItemStore) ItemSubscription() chan []Item {
	itemChan := make(chan []Item)
	s.itemChans = append(s.itemChans, itemChan)
	return itemChan
}

// Cancels a subscription for an item
func (s *ItemStore) CancelSubscription(itemChan chan []Item) {
	for i := len(s.itemChans); i >= 0; i-- {
		if itemChan == s.itemChans[i] {
			s.itemChans = append(s.itemChans[:i], s.itemChans[i+1:]...)
		}
	}
}

// Starts the item store running. This periodically refreshes the items in the store.
func (s *ItemStore) Start() error {
	s.fetcher = ItemFetcher{}
	err := s.fetcher.Start()
	if err != nil {
		return fmt.Errorf("could not initialize item fetcher: %w", err)
	}
	// Initial population at first start
	s.refresh()

	// Worker which periodically refreshes the items in the store
	go func() {
		for {
			select {
			case <-time.After(time.Minute * 2):
				log.Infof("item store refreshing")
				s.refresh()
				log.Infof("item store refreshed with %d items", len(s.items))
			case <-s.stop:
				log.Infof("item store refresh worker exiting")
				return
			}
		}
	}()

	return nil
}

// Stops the item store refresh worker
func (s *ItemStore) Stop() {
	s.fetcher.Stop()
	s.stop <- struct{}{}
	<-s.stopped
}

// Encapsulates a Selenium service to fetch items while reusing the same webdriver
type ItemFetcher struct {
	seleniumSvc *selenium.Service
	driver      selenium.WebDriver
}

func (i *ItemFetcher) Start() error {
	var err error
	i.seleniumSvc, err = selenium.NewChromeDriverService("/usr/bin/chromedriver", 4444)
	if err != nil {
		return fmt.Errorf("could not start chromedriver: %w", err)
	}
	capabilities := selenium.Capabilities{"browser": "chrome"}
	capabilities.AddChrome(chrome.Capabilities{Args: []string{"--headless"}})
	i.driver, err = selenium.NewRemote(capabilities, "http://127.0.0.1:4444/wd/hub")
	if err != nil {
		return fmt.Errorf("could not start selenium: %w", err)
	}
	return nil
}

func (i *ItemFetcher) Stop() {
	_ = i.seleniumSvc.Stop()
	_ = i.driver.Quit()
}

// Fetches Amazon items from a search with Chromedriver via Selenium
// Page begins at 1
func (i *ItemFetcher) FetchItems(page int) ([]Item, error) {
	// Get the page source
	baseURL, err := url.Parse("https://www.amazon.com/s/ref=sr_st_featured-rank")
	if err != nil {
		panic(err)
	}
	params := url.Values{}
	for k, v := range map[string]string{
		"bbn":  "595048",
		"fst":  "as:off",
		"lo":   "computers",
		"qid":  "1526155460",
		"rh":   "n:172282,n:!493964,n:541966,n:1292110011,n:595048,p_n_feature_two_browse-bin:5446816011",
		"sort": "featured-rank",
		"page": strconv.Itoa(page),
	} {
		params.Add(k, v)
	}
	baseURL.RawQuery = params.Encode()
	if err := i.driver.Get(baseURL.String()); err != nil {
		return nil, fmt.Errorf("could not fetch amazon page: %w", err)
	}
	// This page load check makes sure the bottom 'Next Page' button appears in the doc before continuing.
	// This is necessary because I observed sometimes the document would truncate halfway through the item list
	err = i.driver.Wait(func(wd selenium.WebDriver) (b bool, err error) {
		pageSource, err := wd.PageSource()
		if err != nil {
			return true, err
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(pageSource))
		if err != nil {
			return false, fmt.Errorf("page load check could not parse source: %w", err)
		}
		return doc.Find("li.a-last").Length() > 0, nil
	})
	if err != nil {
		return nil, fmt.Errorf("could not wait for doc load: %w", err)
	}

	// Parse the page source
	html, err := i.driver.PageSource()
	if err != nil {
		return nil, fmt.Errorf("could not retrieve source: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return nil, fmt.Errorf("could not parse source: %w", err)
	}
	var items []Item
	doc.Find("div[data-asin]").Each(func(i int, itemObj *goquery.Selection) {
		asin, ok := itemObj.Attr("data-asin")
		if !ok {
			log.Tracef("skipping idx %d: no ASIN", i)
			return
		}
		itemUrl := "https://amazon.com/dp/" + asin
		name := itemObj.Find("span.a-text-normal").Text()
		priceObj := itemObj.Find("span.a-price > span > span")
		if priceObj == nil {
			log.Tracef("skipping %#v: no price tag", asin)
			return
		}
		priceStr := priceObj.Text()
		if len(priceStr) == 0 || priceStr[0] != '$' {
			log.Tracef("skipping %#v: invalid price string", asin)
			return
		}
		priceFloat, err := strconv.ParseFloat(strings.ReplaceAll(priceStr[1:], ",", ""), 32)
		if err != nil {
			log.Tracef("skipping %#v: price is not a float", asin)
			return
		}
		price := float32(priceFloat)
		capacityMatch := reCapacity.FindAllStringSubmatch(name, -1)
		if len(capacityMatch) != 1 || len(capacityMatch[0]) == 0 {
			log.Tracef("skipping %#v: title lacks a capacity", asin)
			return
		}
		capacityFloat, err := strconv.ParseFloat(capacityMatch[0][1], 32)
		if err != nil {
			log.Tracef("skipping %#v: capacity is not a float", asin)
			return
		}
		capacity := float32(capacityFloat)
		item := Item{
			ASIN:       asin,
			URL:        itemUrl,
			Name:       name,
			Price:      price,
			Capacity:   capacity,
			Efficiency: price / capacity,
		}
		items = append(items, item)
	})

	return items, nil
}

func main() {
	log.Infof("api initializing")
	itemStore := ItemStore{}
	err := itemStore.Start()
	if err != nil {
		log.Fatal(err)
	}

	upgrader := &websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("could not upgrade websocket: %s", err.Error())
			return
		}
		defer ws.Close()

		// Feed the websocket with item information and continue to refresh it as new item information rolls in
		remoteAddr := ws.RemoteAddr().String()
		log.Debugf("starting websocket producer for host %s", remoteAddr)
		err = ws.WriteJSON(itemStore.Items())
		if err != nil {
			log.Infof("disconnecting host %s: could not populate initial items: %s", remoteAddr, err.Error())
		}
		itemChan := itemStore.ItemSubscription()
		defer itemStore.CancelSubscription(itemChan)
		for {
			items := <-itemChan
			log.Debugf("sending %d new items to host %s", len(items), remoteAddr)
			err := ws.WriteJSON(items)
			if err != nil {
				log.Infof("disconnecting host %s: could not update items: %s", remoteAddr, err.Error())
			}
		}
	})

	err = http.ListenAndServe("127.0.0.1:3001", nil)
	if err == nil {
		log.Infof("api shutting down")
	} else {
		log.Infof("api shutting down: %s", err.Error())
	}
}
