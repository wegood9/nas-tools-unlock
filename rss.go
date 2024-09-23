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

var queueMap = struct {
	sync.RWMutex
	queues map[string]*FixedCapacityQueue
}{queues: make(map[string]*FixedCapacityQueue)}

func main() {
	e := echo.New()

	// Global queue for default /new and /rss
	dataQueue := NewFixedCapacityQueue(100)
	queueMap.queues["default"] = dataQueue

	// Define a route to handle POST requests with JSON data for the default endpoint
	e.POST("/new", func(c echo.Context) error {
		return handleNewRequest(c, "default")
	})

	e.GET("/rss", func(c echo.Context) error { return handleRSSRequest(c, "default") })

	// Dynamic route handler for creating new endpoints
	e.POST("/:queue_name/new", func(c echo.Context) error {
		queueName := c.Param("queue_name")

		// Check if the queue already exists
		queueMap.Lock()
		if _, exists := queueMap.queues[queueName]; !exists {
			// Create a new queue for this endpoint
			queueMap.queues[queueName] = NewFixedCapacityQueue(100)
			log.Printf("Created new queue for %s", queueName)
		}
		queueMap.Unlock()

		return handleNewRequest(c, queueName)
	})

	e.GET("/:queue_name/rss", func(c echo.Context) error {
		queueName := c.Param("queue_name")

		// Check if the queue exists
		queueMap.RLock()
		if _, exists := queueMap.queues[queueName]; !exists {
			queueMap.RUnlock()
			return c.String(http.StatusNotFound, "Queue not found")
		}
		queueMap.RUnlock()

		return handleRSSRequest(c, queueName)
	})

	// Start the server
	err := e.Start(":3004")
	if err != nil {
		log.Fatal(err)
	}
}

// Helper function to handle new requests (POST /new)
func handleNewRequest(c echo.Context, queueName string) error {
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

	// Add the item to the appropriate queue
	queueMap.Lock()
	queue := queueMap.queues[queueName]
	queue.Enqueue(item)
	queueMap.Unlock()

	log.Printf("Added item to %s: %s", queueName, data.Title)
	return c.NoContent(http.StatusOK)
}

// Helper function to handle RSS requests (GET /rss)
func handleRSSRequest(c echo.Context, queueName string) error {
	// Create the RSS channel for the requested queue
	feedChannel, _ := rss2.NewChannel(channelTitle, channelLink, channelDesc)
	// Retrieve the queue and add its items to the RSS feed
	queueMap.RLock()
	feedChannel.Items = queueMap.queues[queueName].Traverse()
	queueMap.RUnlock()

	// Return the RSS feed in XML format
	return c.XML(http.StatusOK, rss2.NewRSS(feedChannel))
}
