package main

import (
	"github.com/araddon/dateparse"
	"github.com/codesoap/rss2"
	"github.com/labstack/echo"
	"log"
	"net/http"
	"sync"
)

const (
	channelTitle = "Aggregated Torrents"
	channelLink  = "http://example.com"
	channelDesc  = "Self-hosted torrents RSS server"
)

type Data struct {
	Title       string `json:"title"`
	Enclosure   string `json:"enclosure"`
	Size        int    `json:"size"`
	Description string `json:"description"`
	Link        string `json:"link"`
	Guid        string `json:"guid"`
	Pubdate     string `json:"pubdate"`
}

type FixedCapacityQueue struct {
	capacity int
	queue    []interface{}
	size     int
	head     int
	tail     int
	mutex    sync.Mutex
}

func NewFixedCapacityQueue(capacity int) *FixedCapacityQueue {
	return &FixedCapacityQueue{
		capacity: capacity,
		queue:    make([]interface{}, capacity),
		size:     0,
		head:     0,
		tail:     0,
	}
}

func (q *FixedCapacityQueue) Enqueue(item interface{}) {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	if q.size == q.capacity {
		// The queue is full, remove the oldest element by advancing the head index.
		q.head = (q.head + 1) % q.capacity
		q.size--
	}

	q.queue[q.tail] = item
	q.tail = (q.tail + 1) % q.capacity
	q.size++
}

func (q *FixedCapacityQueue) Traverse() []*rss2.Item {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	result := make([]*rss2.Item, q.size)
	for i := 0; i < q.size; i++ {
		result[q.size-i-1] = q.queue[(q.head+i)%q.capacity].(*rss2.Item)
	}
	return result
}

func (q *FixedCapacityQueue) Size() int {
	q.mutex.Lock()
	defer q.mutex.Unlock()

	return q.size
}

func main() {
	e := echo.New()

	dataQueue := NewFixedCapacityQueue(100)

	// Define a route to handle POST requests with JSON data
	e.POST("/new", func(c echo.Context) error {
		data := new(Data)

		// Bind the JSON data from the request body to the Data struct
		if err := c.Bind(data); err != nil {
			log.Println(err.Error())
			return c.String(http.StatusBadRequest, err.Error())
		}

		parsedTime, err := dateparse.ParseStrict(data.Pubdate)
		if err != nil {
			log.Println(err.Error())
			return c.String(http.StatusBadRequest, err.Error())
		}

		item, _ := rss2.NewItem(data.Title, data.Description)
		item.Link = data.Link
		item.PubDate = &rss2.RSSTime{Time: parsedTime}
		guid, _ := rss2.NewGUID(data.Guid)
		item.GUID = guid
		enclosure, _ := rss2.NewEnclosure(data.Enclosure, data.Size, "application/x-bittorrent")
		item.Enclosure = enclosure

		dataQueue.Enqueue(item)
		log.Println(data.Title)
		return c.NoContent(http.StatusOK)
	})

	e.GET("/rss", func(c echo.Context) error {

		feedChannel, _ := rss2.NewChannel(channelTitle, channelLink, channelDesc)
		feedChannel.Items = dataQueue.Traverse()

		return c.XML(http.StatusOK, rss2.NewRSS(feedChannel))
	})

	err := e.Start(":3004")
	if err != nil {
		println(err.Error())
	}
}
