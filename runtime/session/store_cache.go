package session

import (
	"container/list"
	"time"

	"github.com/lengzhao/agentkit"
)

type cacheItem struct {
	id       agentkit.SessionID
	sess     *JSONL
	lastUsed time.Time
}

// sessionCache tracks hot JSONL sessions in LRU order. It is not concurrency-safe;
// Store.mu must be held.
type sessionCache struct {
	maxSize int
	idleTTL time.Duration
	now     func() time.Time
	items   map[agentkit.SessionID]*list.Element
	order   *list.List
}

func newSessionCache(maxSize int, idleTTL time.Duration, now func() time.Time) sessionCache {
	if now == nil {
		now = time.Now
	}
	return sessionCache{
		maxSize: maxSize,
		idleTTL: idleTTL,
		now:     now,
		items:   make(map[agentkit.SessionID]*list.Element),
		order:   list.New(),
	}
}

func (c *sessionCache) len() int {
	return len(c.items)
}

func (c *sessionCache) get(id agentkit.SessionID) (*JSONL, bool) {
	el, ok := c.items[id]
	if !ok {
		return nil, false
	}
	item := el.Value.(*cacheItem)
	now := c.now()
	item.lastUsed = now
	c.order.MoveToBack(el)
	c.evict(now)
	return item.sess, true
}

func (c *sessionCache) put(id agentkit.SessionID, sess *JSONL) {
	now := c.now()
	if el, ok := c.items[id]; ok {
		item := el.Value.(*cacheItem)
		item.sess = sess
		item.lastUsed = now
		c.order.MoveToBack(el)
		c.evict(now)
		return
	}
	el := c.order.PushBack(&cacheItem{
		id:       id,
		sess:     sess,
		lastUsed: now,
	})
	c.items[id] = el
	c.evict(now)
}

func (c *sessionCache) evict(now time.Time) {
	if c.idleTTL > 0 {
		for el := c.order.Front(); el != nil; {
			next := el.Next()
			item := el.Value.(*cacheItem)
			if now.Sub(item.lastUsed) >= c.idleTTL {
				c.remove(el)
			}
			el = next
		}
	}
	for c.maxSize > 0 && c.len() > c.maxSize {
		front := c.order.Front()
		if front == nil {
			return
		}
		c.remove(front)
	}
}

func (c *sessionCache) remove(el *list.Element) {
	item := el.Value.(*cacheItem)
	delete(c.items, item.id)
	c.order.Remove(el)
}
