package service

import (
	"container/list"

	"github.com/wakewon/bob-plugin-mdict/internal/entryir"
)

// entryCacheKey identifies one parsed duplicate-aware entry set.
type entryCacheKey struct {
	dictionaryID string
	// query is TrimSpace + NFC, preserving case because differently cased
	// headwords may be distinct dictionary entries.
	query       string
	maxExamples int
	debug       bool
}

// entryCache is a small LRU over parsed entries. Interactive lookup repeats the
// same word constantly — re-selecting it, replaying audio — and parsing a large
// entry is the most expensive step in the pipeline.
type entryCache struct {
	capacity int
	order    *list.List
	items    map[entryCacheKey]*list.Element
}

type cacheItem struct {
	key entryCacheKey
	set *entryir.EntrySet
}

func newEntryCache(capacity int) *entryCache {
	if capacity <= 0 {
		capacity = 64
	}
	return &entryCache{
		capacity: capacity,
		order:    list.New(),
		items:    make(map[entryCacheKey]*list.Element, capacity),
	}
}

func (c *entryCache) get(key entryCacheKey) (*entryir.EntrySet, bool) {
	element, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(element)
	return element.Value.(*cacheItem).set, true
}

func (c *entryCache) put(key entryCacheKey, set *entryir.EntrySet) {
	if element, ok := c.items[key]; ok {
		element.Value.(*cacheItem).set = set
		c.order.MoveToFront(element)
		return
	}
	element := c.order.PushFront(&cacheItem{key: key, set: set})
	c.items[key] = element
	for c.order.Len() > c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			return
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*cacheItem).key)
	}
}
