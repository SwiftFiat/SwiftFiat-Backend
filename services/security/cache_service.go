package security

import (
	"fmt"
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

type Cache struct {
	init bool
	c    *cache.Cache
}

var (
	CacheInstance Cache
	lock          = &sync.Mutex{}
)

func NewCache() *Cache {
	lock.Lock()
	defer lock.Unlock()

	if !CacheInstance.init {
		CacheInstance = Cache{
			init: true,
		} // <-- thread safe
	}

	return &CacheInstance
}

func (cm *Cache) Start() error {
	// Create a cache with a default expiration time of 5 minutes, and which
	// purges expired items every 10 minutes
	c := cache.New(5*time.Minute, 10*time.Minute)
	cm.c = c
	return nil
}

func (cm *Cache) Insert(k string, x interface{}) {
	cm.c.Set(k, x, cache.DefaultExpiration)
}

func (cm *Cache) Get(key string) (interface{}, error) {
	val, found := cm.c.Get(key)
	if found {
		return val, nil
	}

	return nil, fmt.Errorf("value not found")
}

func (cm *Cache) Stop() error {
	cm.c.Flush()
	return nil
}
