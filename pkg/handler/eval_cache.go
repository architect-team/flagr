package handler

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/checkr/flagr/pkg/config"
	"github.com/checkr/flagr/pkg/entity"
	"github.com/checkr/flagr/pkg/util"

	"github.com/sirupsen/logrus"
	"github.com/zhouzhuojie/withtimeout"
)

var (
	singletonEvalCache     *EvalCache
	singletonEvalCacheOnce sync.Once
)

type cacheContainer struct {
	idCache  map[string]*entity.Flag
	keyCache map[string]*entity.Flag
	tagCache map[string]map[uint]*entity.Flag
}

// EvalCache is the in-memory cache just for evaluation
type EvalCache struct {
	cache           atomic.Value
	refreshTimeout  time.Duration
	refreshInterval time.Duration
}

// GetEvalCache gets the EvalCache
var GetEvalCache = func() *EvalCache {
	singletonEvalCacheOnce.Do(func() {
		ec := &EvalCache{
			refreshTimeout:  config.Config.EvalCacheRefreshTimeout,
			refreshInterval: config.Config.EvalCacheRefreshInterval,
		}
		singletonEvalCache = ec
	})
	return singletonEvalCache
}

// Start starts the polling of EvalCache
func (ec *EvalCache) Start() {
	err := ec.reloadMapCache()
	if err != nil {
		panic(err)
	}
	go func() {
		for range time.Tick(ec.refreshInterval) {
			err := ec.reloadMapCache()
			if err != nil {
				logrus.WithField("err", err).Error("reload evaluation cache error")
			}
		}
	}()
}

func (ec *EvalCache) GetByTags(tags []string) []*entity.Flag {
	results := map[uint]*entity.Flag{}
	cache := ec.cache.Load().(*cacheContainer)
	for _, t := range tags {
		fSet, ok := cache.tagCache[t]
		if ok {
			for fID, f := range fSet {
				results[fID] = f
			}
		}
	}

	values := make([]*entity.Flag, 0, len(results))
	for _, f := range results {
		values = append(values, f)
	}

	return values
}

// GetByFlagKeyOrID gets the flag by Key or ID
func (ec *EvalCache) GetByFlagKeyOrID(keyOrID interface{}) *entity.Flag {
	s := util.SafeString(keyOrID)
	cache := ec.cache.Load().(*cacheContainer)
	f, ok := cache.idCache[s]
	if !ok {
		f = cache.keyCache[s]
	}
	return f
}

func (ec *EvalCache) reloadMapCache() error {
	if config.Config.NewRelicEnabled {
		defer config.Global.NewrelicApp.StartTransaction("eval_cache_reload", nil, nil).End()
	}

	_, _, err := withtimeout.Do(ec.refreshTimeout, func() (interface{}, error) {
		idCache, keyCache, tagCache, err := ec.fetchAllFlags()
		if err != nil {
			return nil, err
		}

		ec.cache.Store(&cacheContainer{
			idCache:  idCache,
			keyCache: keyCache,
			tagCache: tagCache,
		})
		return nil, err
	})

	return err
}
