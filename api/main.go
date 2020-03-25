package main

import (
	"github.com/PuerkitoBio/goquery"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"github.com/tebeka/selenium"
	"github.com/tebeka/selenium/chrome"
	"io/ioutil"
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
	var sortedItems []Item
	for page := 1; page < 2; page++ {
		sortedItems = append(sortedItems, s.fetcher.FetchItems(page)...)
	}
	sort.Slice(sortedItems, func(i, j int) bool {
		return sortedItems[i].Efficiency < sortedItems[j].Efficiency
	})
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
func (s *ItemStore) Start() {
	s.fetcher = ItemFetcher{}
	s.fetcher.Start()

	// Initial population at first start
	s.refresh()

	// Worker which periodically refreshes the items in the store
	go func() {
		for {
			select {
			case <-time.After(time.Minute):
				log.Infof("Item store refreshing")
				s.refresh()
				log.Infof("Item store refreshed with %d items", len(s.items))
			case <-s.stop:
				log.Infof("Item store refresh worker exiting")
				return
			}
		}
	}()
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

func (i *ItemFetcher) Start() {
	var err error
	i.seleniumSvc, err = selenium.NewChromeDriverService("/usr/bin/chromedriver", 4444)
	if err != nil {
		panic(err)
	}
	capabilities := selenium.Capabilities{"browser": "chrome"}
	capabilities.AddChrome(chrome.Capabilities{Args: []string{"--headless"}})
	i.driver, err = selenium.NewRemote(capabilities, "http://127.0.0.1:4444/wd/hub")
	if err != nil {
		panic(err)
	}
}

func (i *ItemFetcher) Stop() {
	i.seleniumSvc.Stop()
	i.driver.Quit()
}

// Fetches Amazon items from a search with Chromedriver via Selenium
// Page begins at 1
func (i *ItemFetcher) FetchItems(page int) []Item {

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
		panic(err)
	}
	i.driver.Wait(func(wd selenium.WebDriver) (b bool, err error) {
		pageSource, err := wd.PageSource()
		if err != nil {
			return true, err
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(pageSource))
		return doc.Find("li.a-last").Length() > 0, nil
	})
	html, err := i.driver.PageSource()
	if err != nil {
		panic(err)
	}
	err = ioutil.WriteFile("/tmp/random", []byte(html), 0o644)
	if err != nil {
		panic(err)
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		panic(err)
	}

	var items []Item
	doc.Find("div[data-asin]").Each(func(i int, itemObj *goquery.Selection) {
		asin, ok := itemObj.Attr("data-asin")
		if !ok {
			log.Tracef("Skipping idx %d: no ASIN", i)
			return
		}
		url := "https://amazon.com/dp/" + asin
		name := itemObj.Find("span.a-text-normal").Text()
		priceObj := itemObj.Find("span.a-price > span > span")
		if priceObj == nil {
			log.Tracef("Skipping %#v: no price tag", asin)
			return
		}
		priceStr := priceObj.Text()
		if len(priceStr) == 0 || priceStr[0] != '$' {
			log.Tracef("Skipping %#v: invalid price string", asin)
			return
		}
		priceFloat, err := strconv.ParseFloat(strings.ReplaceAll(priceStr[1:], ",", ""), 32)
		if err != nil {
			log.Tracef("Skipping %#v: price is not a float", asin)
			return
		}
		price := float32(priceFloat)
		capacityMatch := reCapacity.FindAllStringSubmatch(name, -1)
		if len(capacityMatch) != 1 || len(capacityMatch[0]) == 0 {
			log.Tracef("Skipping %#v: title lacks a capacity", asin)
			return
		}
		capacityFloat, err := strconv.ParseFloat(capacityMatch[0][1], 32)
		if err != nil {
			log.Tracef("Skipping %#v: capacity is not a float", asin)
			return
		}
		capacity := float32(capacityFloat)
		item := Item{
			ASIN:       asin,
			URL:        url,
			Name:       name,
			Price:      price,
			Capacity:   capacity,
			Efficiency: price / capacity,
		}
		items = append(items, item)
	})

	return items
}

func main() {
	itemStore := ItemStore{}
	itemStore.Start()

	upgrader := &websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("Could not upgrade websocket: %s", err.Error())
			return
		}
		defer ws.Close()

		// Feed the websocket with item information and continue to refresh it as new item information rolls in
		remoteAddr := ws.RemoteAddr().String()
		log.Debugf("Starting websocket producer for host %s", remoteAddr)
		err = ws.WriteJSON(itemStore.Items())
		if err != nil {
			log.Infof("Disconnecting host %s: Could not populate initial items: %s", remoteAddr, err.Error())
		}
		itemChan := itemStore.ItemSubscription()
		defer itemStore.CancelSubscription(itemChan)
		for {
			items := <-itemChan
			log.Debugf("Sending %d new items to host %s", len(items), remoteAddr)
			err := ws.WriteJSON(items)
			if err != nil {
				log.Infof("Disconnecting host %s: Could not update items: %s", remoteAddr, err.Error())
			}
		}
	})

	http.ListenAndServe("0.0.0.0:8080", nil)
}
