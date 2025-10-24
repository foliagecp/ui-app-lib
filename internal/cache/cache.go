package cache

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"time"

	"github.com/foliagecp/easyjson"
	lg "github.com/foliagecp/sdk/statefun/logger"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/nats-io/nats.go"
)

var (
	uiCache *Cache
)

type Cache struct {
	cache        sync.Map
	pending      sync.Map
	correlator   map[string]string
	mu           sync.RWMutex
	config       *Config
	subscription *nats.Subscription
	nc           *nats.Conn
}

type CacheEntry struct {
	EgressPayloads []*easyjson.JSON
	ExpiresAt      time.Time
}

type PendingEntry struct {
	TraceID       string
	Hash          string
	Payloads      []*easyjson.JSON
	FirstEgressAt time.Time
	Timer         *time.Timer
	Mutex         sync.Mutex
}

func PrepareCollection(traceID, hash string) bool {
	if uiCache == nil {
		return false
	}

	uiCache.mu.Lock()
	uiCache.correlator[traceID] = hash
	uiCache.mu.Unlock()

	entry := &PendingEntry{
		TraceID:       traceID,
		Hash:          hash,
		Payloads:      []*easyjson.JSON{},
		FirstEgressAt: time.Now(),
	}

	entry.Timer = time.AfterFunc(
		time.Duration(uiCache.config.CollectTimeoutMS)*time.Millisecond,
		func() { saveToCache(hash) },
	)

	_, loaded := uiCache.pending.LoadOrStore(hash, entry)

	if loaded {
		entry.Timer.Stop()
		uiCache.mu.Lock()
		delete(uiCache.correlator, traceID)
		uiCache.mu.Unlock()
		return false
	}

	return true
}

func saveToCache(hash string) {
	if uiCache == nil {
		return
	}

	entryInterface, ok := uiCache.pending.Load(hash)
	if !ok {
		return
	}

	entry := entryInterface.(*PendingEntry)
	entry.Mutex.Lock()

	if entry.Timer == nil {
		entry.Mutex.Unlock()
		return
	}

	payloads := make([]*easyjson.JSON, len(entry.Payloads))
	for i, p := range entry.Payloads {
		payloads[i] = p.Clone().GetPtr()
	}

	traceID := entry.TraceID
	entry.Timer = nil
	entry.Mutex.Unlock()

	if len(payloads) == 0 {
		uiCache.mu.Lock()
		delete(uiCache.correlator, traceID)
		uiCache.mu.Unlock()
		uiCache.pending.Delete(hash)
		return
	}

	uiCache.cache.Store(hash, &CacheEntry{
		EgressPayloads: payloads,
		ExpiresAt:      time.Now().Add(time.Duration(uiCache.config.TTLSeconds) * time.Second),
	})

	uiCache.mu.Lock()
	delete(uiCache.correlator, traceID)
	uiCache.mu.Unlock()

	uiCache.pending.Delete(hash)
}

func CollectEgress(payload *easyjson.JSON) {
	if uiCache == nil {
		return
	}

	if payload.GetByPath("cached").AsBoolDefault(false) {
		return
	}

	traceID := payload.GetByPath("__trace_context.trace_id").AsStringDefault("")

	if traceID == "" {
		return
	}

	if payload.PathExists("payload.command") {
		return
	}

	uiCache.mu.RLock()
	hash, ok := uiCache.correlator[traceID]
	uiCache.mu.RUnlock()

	if !ok {
		return
	}

	entryInterface, ok := uiCache.pending.Load(hash)
	if !ok {
		return
	}

	entry := entryInterface.(*PendingEntry)
	entry.Mutex.Lock()
	defer entry.Mutex.Unlock()

	clone := easyjson.NewJSONObject()
	clone.SetByPath("payload", payload.GetByPath("payload"))
	clone.SetByPath("cached", easyjson.NewJSON(true))
	clone.SetByPath("cache_timestamp", easyjson.NewJSON(time.Now().Unix()))

	entry.Payloads = append(entry.Payloads, &clone)
}

func PublishCachedEgress(clientID string, egressPayloads []*easyjson.JSON) {
	for _, payload := range egressPayloads {
		if err := uiCache.nc.Publish(fmt.Sprintf("egress.ui.%s", clientID), payload.ToBytes()); err != nil {
			lg.GetLogger().Errorf(context.TODO(), "publishCachedEgress error: %v", err)
		}
	}
}

func Get(hash string) *CacheEntry {
	entryInterface, ok := uiCache.cache.Load(hash)
	if !ok {
		return nil
	}

	entry := entryInterface.(*CacheEntry)

	if time.Now().Before(entry.ExpiresAt) {
		return entry
	}

	uiCache.cache.Delete(hash)
	return nil
}

func IsCacheable(payload *easyjson.JSON) bool {
	if !Enabled() {
		return false
	}

	if payload.PathExists("command") {
		return false
	}

	return payload.PathExists("viewer") || payload.PathExists("controllers")
}

func Hash(payload *easyjson.JSON) string {
	clone := payload.Clone().GetPtr()
	clone.Normalize()
	bytes := clone.ToBytes()
	h := fnv.New64a()
	system.MsgOnErrorReturn(h.Write(bytes))
	return strconv.FormatUint(h.Sum64(), 16)
}

func (c *Cache) handleNatsMessage(msg *nats.Msg) {
	payload, ok := easyjson.JSONFromBytes(msg.Data)
	if !ok {
		lg.GetLogger().Errorf(context.TODO(), "invalid nats message")
	}

	CollectEgress(&payload)
}
