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
	PERIODIC_CLEANER_TIMEOUT    = time.Minute * 1
)

var config *Config

type Config struct {
	Enabled          bool
	TTLSeconds       int
	CollectTimeoutMS int
	MaxEntries       int
}

func InitConfig() {
	config = &Config{
		Enabled:          system.GetEnvMustProceed("UI_APP_LIB_CACHE_ENABLED", true),
		TTLSeconds:       system.GetEnvMustProceed("UI_APP_LIB_CACHE_TTL_SECONDS", 600),
		CollectTimeoutMS: system.GetEnvMustProceed("UI_APP_LIB_CACHE_COLLECT_TIMEOUT_MS", 10000),
		MaxEntries:       system.GetEnvMustProceed("UI_APP_LIB_CACHE_MAX_ENTRIES", 1000),
	}
}

func Init(runtime *statefun.Runtime) {
	le := lg.GetLogger()
	InitConfig()

	if !config.Enabled {
		le.Infof(context.TODO(), "cache disabled")
		return
	}

	nc := runtime.GetNatsConnection()

	cache := &Cache{
		correlator:     make(map[string]CorrelatorEntry),
		correlatorMu:   sync.RWMutex{},
		egressPayloads: make(map[string]*easyjson.JSON),
		config:         config,
		nc:             nc,
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

	// start periodic cache cleaner
	go func() {
		ticker := time.NewTicker(PERIODIC_CLEANER_TIMEOUT)
		for range ticker.C {
			if uiCache == nil {
				continue
			}
			deleted, all := 0, 0
			lg.GetLogger().Tracef(context.TODO(), ">>>>>>>>>>>>>>>>>>>>>>>>>>> start delete old entries from ui-cache >>>>>>>>>>>>>>>>>>>>>>>>>>>")
			uiCache.cache.Range(func(key, value interface{}) bool {
				entry := value.(*CacheEntry)
				all++
				if time.Now().After(entry.ExpiresAt) {
					uiCache.cache.Delete(key)
					deleted++
				}
				return true
			})
			if all-deleted > config.MaxEntries {
				lg.GetLogger().Warnf(context.TODO(), "ui-cache reached MaxSize (%d), current count=%d", config.MaxEntries, all-deleted)
			}
			lg.GetLogger().Tracef(context.TODO(), "<<<<<<<<<<<<<<<<<<<<<<<<<<<< finish delete old entries from ui-cache, all=%d, entries was deleted=%d <<<<<<<<<<<<<<<<<<<<<<<<<<<<", all, deleted)

			all, deleted = 0, 0
			lg.GetLogger().Tracef(context.TODO(), ">>>>>>>>>>>>>>>>>>>>>>>>>>> start delete unactual entries from ui-cache-corellator >>>>>>>>>>>>>>>>>>>>>>>>>>>")
			uiCache.correlatorMu.Lock()
			for traceID := range uiCache.correlator {
				all++
				if _, exists := uiCache.pending.Load(uiCache.correlator[traceID].Hash); !exists {
					delete(uiCache.correlator, traceID)
					deleted++
				}
			}
			uiCache.correlatorMu.Unlock()
			lg.GetLogger().Tracef(context.TODO(), "<<<<<<<<<<<<<<<<<<<<<<<<<<<< finish delete unactual entries from ui-cache-corellator, all=%d, entries was deleted=%d <<<<<<<<<<<<<<<<<<<<<<<<<<<<", all, deleted)

			all, deleted = 0, 0
			lg.GetLogger().Tracef(context.TODO(), ">>>>>>>>>>>>>>>>>>>>>>>>>>> start delete unactual entries from ui-cache-egress-payloads >>>>>>>>>>>>>>>>>>>>>>>>>>>")
			uiCache.egressPayloadsMu.Lock()
			for controllerOID := range uiCache.egressPayloads {
				all++
				used := false
				uiCache.cache.Range(func(_, v interface{}) bool {
					entry := v.(*CacheEntry)
					for _, oid := range entry.ControllerOIDs {
						if oid == controllerOID {
							used = true
							return false
						}
					}
					return true
				})
				if !used {
					delete(uiCache.egressPayloads, controllerOID)
					deleted++
				}
			}
			uiCache.egressPayloadsMu.Unlock()
			lg.GetLogger().Tracef(context.TODO(), "<<<<<<<<<<<<<<<<<<<<<<<<<<<< finish delete unactual entries from ui-cache-egress-payloads, all=%d, entries was deleted=%d <<<<<<<<<<<<<<<<<<<<<<<<<<<<", all, deleted)
		}
	}()

	return
}

func Enabled() bool {
	return config != nil && config.Enabled
}
