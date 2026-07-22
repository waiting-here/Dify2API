package mailer

import (
	"fmt"
	"log"
	"sync"
	"time"

	"dify2api/config"
)

// coolWindow is the aggregation period for each event type.
var coolWindow = 10 * time.Minute

type coolerItem struct {
	at      time.Time
	summary string
}

// cooler buffers events of a single EventType and sends one aggregated
// email after the cooling window expires.
type cooler struct {
	mu        sync.Mutex
	eventType EventType
	items     []coolerItem
	timer     *time.Timer
	cfg       config.SMTPConfig
	sendFunc  func(config.SMTPConfig, string, string) error
}

func newCooler(et EventType, cfg config.SMTPConfig, sendFn func(config.SMTPConfig, string, string) error) *cooler {
	return &cooler{
		eventType: et,
		cfg:       cfg,
		sendFunc:  sendFn,
	}
}

// add appends a summary and starts the flush timer on the first event.
func (c *cooler) add(summary string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = append(c.items, coolerItem{at: time.Now(), summary: summary})

	if c.timer == nil {
		c.timer = time.AfterFunc(coolWindow, func() {
			c.flush()
		})
	}
}

// flush sends one aggregated email and resets for the next window.
func (c *cooler) flush() {
	c.mu.Lock()
	items := c.items
	c.items = nil
	c.timer = nil
	c.mu.Unlock()

	if len(items) == 0 {
		return
	}

	startTime := items[0].at
	endTime := items[len(items)-1].at

	subject := eventSubject(c.eventType, len(items))
	body := fmt.Sprintf("时间范围：%s — %s\r\n\r\n",
		startTime.Format("2006-01-02 15:04:05"),
		endTime.Format("2006-01-02 15:04:05"))
	for _, item := range items {
		body += fmt.Sprintf("- %s %s\r\n", item.at.Format("15:04:05"), item.summary)
	}

	if err := c.sendFunc(c.cfg, subject, body); err != nil {
		log.Printf("[MAILER] send %s failed: %v", c.eventType, err)
	}
}
