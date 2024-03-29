package pkg

import (
	"context"
	"errors"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/chromedp/chromedp"
	"github.com/gorilla/websocket"
	log "github.com/sirupsen/logrus"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	// The global default item store
	itemStore = ItemStore{}
	// Extracts an item's size (in TB) from its title
	reCapacity = regexp.MustCompile("(?i)(\\d+\\.?\\d*?).?TB")
	// Denotes an error while fetching the most recent Amazon item listings
	errFetchFailure = errors.New("could not fetch results")
)

// Represents a scraped Amazon storage item
type Item struct {
	ASIN             string  `json:"asin"`
	URL              string  `json:"url"`
	Name             string  `json:"name"`
	Price            float32 `json:"price"`
	Capacity         float32 `json:"capacity"`
	PricePerTerabyte float32 `json:"price-per-terabyte"`
}

type ItemStore struct {
	fetcher   ItemFetcher
	items     []Item
	itemChans []chan []Item

	stop    chan struct{}
	stopped chan struct{}
}

// Refreshes the items in the store, returning a list sorted by cost efficiency
func (s *ItemStore) refresh(numPages int) error {
	var sortedItems []Item
	for page := 0; page < numPages; page++ {
		itemPage, err := s.fetcher.FetchItems(page)
		if err != nil {
			return err
		}
		sortedItems = append(sortedItems, itemPage...)
	}
	sort.Slice(sortedItems, func(i, j int) bool {
		return sortedItems[i].PricePerTerabyte < sortedItems[j].PricePerTerabyte
	})
	s.items = sortedItems

	// Sends the items to each listening item channel
	for _, itemChan := range s.itemChans {
		itemChan <- s.items
	}

	return nil
}

// Fetches current items in the store
func (s *ItemStore) Items() []Item {
	return s.items
}

// Returns a channel that will output the item list repeatedly as it is updated
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
// refreshPeriod is in seconds
func (s *ItemStore) Start(refreshPeriod, numPages int) error {
	s.fetcher = ItemFetcher{}
	err := s.fetcher.Start()
	if err != nil {
		return err
	}

	// Worker which periodically refreshes the items in the store
	go func() {
		for {
			log.Infof("item store refreshing")
			err = s.refresh(numPages)
			if err == nil {
				log.Infof("item store refreshed with %d items", len(s.items))
			} else {
				log.Errorf("item store could not refresh: %s", err.Error())
			}
			select {
			case <-time.After(time.Second * time.Duration(refreshPeriod)):
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

// Encapsulates a remote browser service to fetch items
type ItemFetcher struct {
	ctx    context.Context
	cancel context.CancelFunc
}

func (i *ItemFetcher) Start() error {
	i.ctx, i.cancel = chromedp.NewContext(context.Background())
	return nil
}

func (i *ItemFetcher) Stop() {
	i.cancel()
}

// Fetches Amazon items from a search page via chromedp
// Page numbers begin at 1
func (i *ItemFetcher) FetchItems(page int) ([]Item, error) {
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
	var html string
	if err := chromedp.Run(i.ctx, chromedp.Navigate(baseURL.String()), chromedp.OuterHTML("html", &html)); err != nil {
		return []Item{}, fmt.Errorf("%w: %s", errFetchFailure, err.Error())
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return []Item{}, fmt.Errorf("%w: %s", errFetchFailure, err.Error())
	}

	var items []Item
	doc.Find("div[data-asin]").Each(func(i int, itemObj *goquery.Selection) {
		asin, ok := itemObj.Attr("data-asin")
		if !ok {
			log.Tracef("Skipping idx %d: no ASIN", i)
			return
		}
		itemUrl := "https://amazon.com/dp/" + asin
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
			ASIN:             asin,
			URL:              itemUrl,
			Name:             name,
			Price:            price,
			Capacity:         capacity,
			PricePerTerabyte: price / capacity,
		}
		items = append(items, item)
	})

	return items, nil
}

// Handles incoming websocket connections
func handleWebsocket(w http.ResponseWriter, r *http.Request) {
	upgrader := &websocket.Upgrader{CheckOrigin: func(r *http.Request) bool {
		return true
	}}

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
}

// Start and serve the hddcheap API
func Serve(refreshPeriod, numPages int, listenAddr string) {
	log.Infof("starting hddcheap api")
	err := itemStore.Start(refreshPeriod, numPages)
	if err != nil {
		log.Fatalf("could not start hddcheap api: %s", err.Error())
	}
	http.HandleFunc("/ws", handleWebsocket)
	_ = http.ListenAndServe(listenAddr, nil)
}
