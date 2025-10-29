package cache

import (
	"context"
	"sync"
	"time"

	"github.com/foliagecp/easyjson"
	"github.com/foliagecp/sdk/statefun"
	lg "github.com/foliagecp/sdk/statefun/logger"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/nats-io/nats.go"
)

const (
	EGRESS_UI_SUBSRIBE_WILDCARD = "egress.ui.>"
)

var config *Config

type Config struct {
	Enabled                   bool
	TTLSeconds                int
	CollectTimeoutMS          int
	MaxEntries                int
	PeriodicCleanerTimeoutMin int
}

func InitConfig() {
	config = &Config{
		Enabled:                   system.GetEnvMustProceed("UI_APP_LIB_CACHE_ENABLED", false),
		TTLSeconds:                system.GetEnvMustProceed("UI_APP_LIB_CACHE_TTL_SECONDS", 600),
		CollectTimeoutMS:          system.GetEnvMustProceed("UI_APP_LIB_CACHE_COLLECT_TIMEOUT_MS", 10000),
		MaxEntries:                system.GetEnvMustProceed("UI_APP_LIB_CACHE_MAX_ENTRIES", 10000),
		PeriodicCleanerTimeoutMin: system.GetEnvMustProceed("UI_APP_LIB_CACHE_PERIODIC_CLEANER_TIMEOUT_MIN", 5),
	}
}

func Init(runtime *statefun.Runtime) {
	le := lg.GetLogger()
	InitConfig()

	if !config.Enabled {
		le.Infof(context.TODO(), ":::::: ui cache disabled, use UI_APP_LIB_CACHE_ENABLED in .env")
		return
	}

	nc := runtime.GetNatsConnection()

	cache := &Cache{
		correlator:     make(map[string]CorrelatorEntry),
		correlatorMu:   sync.RWMutex{},
		egressPayloads: make(map[string]*easyjson.JSON),
		config:         config,
		nc:             nc,
		stopCh:         make(chan struct{}),
	}

	sub, err := nc.Subscribe(EGRESS_UI_SUBSRIBE_WILDCARD, func(msg *nats.Msg) {
		cache.handleNatsMessage(msg)
	})
	if err != nil {
		le.Errorf(context.TODO(), "Failed to subscribe to egress.ui.>: %s", err.Error())
		return
	}

	cache.subscription = sub

	uiCache = cache //global ui cache

	go func() {
		lg.GetLogger().Trace(context.TODO(), "cache cleaner started")
		ticker := time.NewTicker(time.Duration(config.PeriodicCleanerTimeoutMin) * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				usedOIDs := cache.cleanupCacheAndCollect()
				cache.cleanupEgressPayloads(usedOIDs)
				cache.cleanupCorrelator()
			case <-cache.stopCh:
				return
			}
		}
	}()

	le.Infof(context.TODO(), ":::::: ui cache enabled")

	return
}

func Enabled() bool {
	return uiCache != nil && config.Enabled
}

func (c *Cache) Shutdown() {
	if c == nil {
		return
	}

	select {
	case <-c.stopCh:
		return
	default:
		close(c.stopCh)
	}

	if c.subscription != nil {
		system.MsgOnErrorReturn(c.subscription.Unsubscribe())
	}
}

func (c *Cache) cleanupCacheAndCollect() (usedOIDs map[string]struct{}) {
	all, deleted := 0, 0
	log := lg.GetLogger()
	log.Debugf(context.TODO(), ">>> start delete old entries from ui-cache >>>")

	usedOIDs = make(map[string]struct{})

	now := time.Now()

	c.cache.Range(func(key, value any) bool {
		all++
		entry := value.(*CacheEntry)

		if now.After(entry.ExpiresAt) {
			c.cache.CompareAndDelete(key, value)
			deleted++
		} else {
			for _, oid := range entry.ControllerOIDs {
				usedOIDs[oid] = struct{}{}
			}
		}

		return true
	})

	log.Debugf(context.TODO(),
		"<<< finish delete old entries from ui-cache, all=%d, deleted=%d <<<", all, deleted)

	return
}

func (c *Cache) cleanupEgressPayloads(usedOIDs map[string]struct{}) {
	//TODO optimize
	//all, deleted := 0, 0
	//log := lg.GetLogger()
	//log.Debugf(context.TODO(), ">>> start delete unactual entries from ui-cache-egress-payloads >>>")
	//
	//c.egressPayloadsMu.Lock()
	//for controllerOID := range c.egressPayloads {
	//	all++
	//	if _, ok := usedOIDs[controllerOID]; !ok {
	//		delete(c.egressPayloads, controllerOID)
	//		deleted++
	//	}
	//}
	//c.egressPayloadsMu.Unlock()
	//
	//log.Debugf(context.TODO(),
	//	"<<< finish delete unactual entries from ui-cache-egress-payloads, all=%d, deleted=%d <<<", all, deleted)
}

func (c *Cache) cleanupCorrelator() {
	//TODO probably repeat function
	all, deleted := 0, 0
	log := lg.GetLogger()
	log.Debugf(context.TODO(), ">>> start delete unactual entries from ui-cache-correlator >>>")

	c.correlatorMu.Lock()
	for traceID, corrEntry := range c.correlator {
		all++
		if _, exists := c.pending.Load(corrEntry.Hash); !exists {
			delete(c.correlator, traceID)
			deleted++
		}
	}
	c.correlatorMu.Unlock()

	log.Debugf(context.TODO(),
		"<<< finish delete unactual entries from ui-cache-correlator, all=%d, deleted=%d <<<", all, deleted)
}
