package mailer

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"dify2api/config"
)

// coolWindow is the aggregation period for each event type (overridden
// via New if SMTP is configured).
var coolWindow = 10 * time.Minute

type coolerItem struct {
	at      time.Time
	summary string
}

// cooler buffers events of a single EventType and sends one aggregated
// email after the cooling window expires.
type cooler struct {
	mu          sync.Mutex
	eventType   EventType
	coolMinutes func() int
	items       []coolerItem
	timer       *time.Timer
	done        chan struct{}
	cfg         config.SMTPConfig
	sendCtx     context.Context
	sendFunc    func(context.Context, config.SMTPConfig, string, string) error
}

func newCooler(et EventType, coolMinutes func() int, cfg config.SMTPConfig, sendCtx context.Context, sendFn func(context.Context, config.SMTPConfig, string, string) error) *cooler {
	return &cooler{
		eventType:   et,
		coolMinutes: coolMinutes,
		cfg:         cfg,
		sendCtx:     sendCtx,
		sendFunc:    sendFn,
	}
}

// add appends a summary and starts the flush timer on the first event.
func (c *cooler) add(summary string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = append(c.items, coolerItem{at: time.Now(), summary: summary})

	if c.timer == nil {
		win := coolWindow
		if c.coolMinutes != nil {
			if cm := c.coolMinutes(); cm > 0 {
				win = time.Duration(cm) * time.Minute
			}
		}
		done := make(chan struct{})
		c.done = done
		c.timer = time.AfterFunc(win, func() {
			c.flush(done)
		})
	}
}

// flush sends one aggregated email and resets for the next window.
func (c *cooler) flush(done chan struct{}) {
	c.mu.Lock()
	items := c.items
	c.items = nil
	c.timer = nil
	c.mu.Unlock()
	c.send(items)
	c.mu.Lock()
	if c.done == done {
		c.done = nil
	}
	close(done)
	c.mu.Unlock()
}

func (c *cooler) send(items []coolerItem) {

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

	if err := c.sendFunc(c.sendCtx, c.cfg, subject, body); err != nil {
		log.Printf("[MAILER] send %s failed: %v", c.eventType, err)
	} else {
		log.Printf("[MAILER] sent %s (%d events): %s", c.eventType, len(items), subject)
	}
}

// shutdown stops the pending timer, flushes buffered items, and returns a
// channel closed after any already-running send finishes.
func (c *cooler) shutdown() <-chan struct{} {
	c.mu.Lock()
	if c.done == nil {
		c.mu.Unlock()
		done := make(chan struct{})
		close(done)
		return done
	}
	done := c.done
	if c.timer != nil && c.timer.Stop() {
		items := c.items
		c.items = nil
		c.timer = nil
		c.mu.Unlock()
		go func() {
			c.send(items)
			c.mu.Lock()
			if c.done == done {
				c.done = nil
			}
			close(done)
			c.mu.Unlock()
		}()
		return done
	}
	c.mu.Unlock()
	return done
}
