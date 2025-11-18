package security

import (
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

func (cm *Cache) Insert(k string, x any) {
	cm.c.Set(k, x, cache.DefaultExpiration)
}

func (cm *Cache) Get(key string) (any, bool) {
	val, found := cm.c.Get(key)
	return val, found
}

func (cm *Cache) Delete(key string) {
	cm.c.Delete(key)
}

func (cm *Cache) Stop() error {
	cm.c.Flush()
	return nil
}
