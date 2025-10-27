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
	sf "github.com/foliagecp/sdk/statefun/plugins"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/nats-io/nats.go"
)

var (
	uiCache *Cache
)

type Cache struct {
	cache            sync.Map                   // hash -> CacheEntry
	pending          sync.Map                   // hash -> CacheEntry (temporary)
	correlator       map[string]CorrelatorEntry // traceID -> hash, controllerOID (temporary)
	correlatorMu     sync.RWMutex               // correlator mutex
	egressPayloads   map[string]*easyjson.JSON  // controllerOID -> egress Payloads
	egressPayloadsMu sync.RWMutex               // egress Payloads mutex
	config           *Config                    // cache config
	subscription     *nats.Subscription         // egress subscription
	nc               *nats.Conn                 // nats connection
}

type CorrelatorEntry struct {
	Hash           string
	ControllerOIDs []string
}

type CacheEntry struct {
	ExpiresAt      time.Time
	ControllerOIDs []string
}

type PendingEntry struct {
	TraceID       string
	Hash          string
	ControllerIDs []string
	FirstEgressAt time.Time
	Timer         *time.Timer
	Mutex         sync.Mutex
}

func PrepareCollection(ctx *sf.StatefunContextProcessor, hash string) bool {
	if !Enabled() {
		return false
	}

	traceID := ctx.TraceID()

	uiCache.correlatorMu.Lock()
	uiCache.correlator[traceID] = CorrelatorEntry{
		Hash:           hash,
		ControllerOIDs: make([]string, 0),
	}
	uiCache.correlatorMu.Unlock()

	entry := &PendingEntry{
		TraceID:       traceID,
		Hash:          hash,
		ControllerIDs: make([]string, 0),
		FirstEgressAt: time.Now(),
	}

	entry.Timer = time.AfterFunc(
		time.Duration(uiCache.config.CollectTimeoutMS)*time.Millisecond,
		func() { saveToCache(hash) },
	)

	_, loaded := uiCache.pending.LoadOrStore(hash, entry)

	if loaded {
		entry.Timer.Stop()
		uiCache.correlatorMu.Lock()
		delete(uiCache.correlator, traceID)
		uiCache.correlatorMu.Unlock()
	}

	return true
}

func saveToCache(hash string) {
	if !Enabled() {
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

	traceID := entry.TraceID
	entry.Timer = nil
	entry.Mutex.Unlock()

	if len(entry.ControllerIDs) == 0 {
		uiCache.correlatorMu.Lock()
		delete(uiCache.correlator, traceID)
		uiCache.correlatorMu.Unlock()
		uiCache.pending.Delete(hash)
		return
	}

	uiCache.cache.Store(hash, &CacheEntry{
		ControllerOIDs: entry.ControllerIDs,
		ExpiresAt:      time.Now().Add(time.Duration(uiCache.config.TTLSeconds) * time.Second),
	})

	uiCache.correlatorMu.Lock()
	delete(uiCache.correlator, traceID)
	uiCache.correlatorMu.Unlock()

	uiCache.pending.Delete(hash)
}

func collectEgress(payload *easyjson.JSON) {
	if !Enabled() {
		return
	}

	if payload.GetByPath("cached").AsBoolDefault(false) {
		return
	}

	traceId, ok := payload.GetByPath("__trace_context.trace_id").AsString()
	if !ok {
		return
	}

	controllerOID, ok := payload.GetByPath("__caller_id").AsString()
	if !ok {
		return
	}

	if _, ok := uiCache.correlator[traceId]; !ok {
		uiCache.egressPayloadsMu.Lock()
		defer uiCache.egressPayloadsMu.Unlock()
		clone := easyjson.NewJSONObject()
		clone.SetByPath("payload", payload.GetByPath("payload"))
		clone.SetByPath("cached", easyjson.NewJSON(true))
		uiCache.egressPayloads[controllerOID] = &clone
		return
	}

	uiCache.correlatorMu.RLock()
	correlatorEntry, ok := uiCache.correlator[traceId]
	uiCache.correlatorMu.RUnlock()
	if !ok {
		return
	}

	pendingEntryInterface, ok := uiCache.pending.Load(correlatorEntry.Hash)
	if !ok {
		return
	}

	pendingEntry := pendingEntryInterface.(*PendingEntry)
	pendingEntry.Mutex.Lock()
	defer pendingEntry.Mutex.Unlock()

	if pendingEntry.Timer != nil {
		pendingEntry.Timer.Reset(time.Duration(uiCache.config.CollectTimeoutMS) * time.Millisecond)
	}

	pendingEntry.ControllerIDs = append(pendingEntry.ControllerIDs, controllerOID)

	clone := easyjson.NewJSONObject()
	clone.SetByPath("payload", payload.GetByPath("payload"))
	clone.SetByPath("cached", easyjson.NewJSON(true))

	uiCache.egressPayloadsMu.Lock()
	uiCache.egressPayloads[controllerOID] = &clone
	uiCache.egressPayloadsMu.Unlock()

	uiCache.pending.Store(correlatorEntry.Hash, pendingEntry)
}

func PublishCachedEgress(clientID string, controllerOIDs []string) {
	if !Enabled() {
		return
	}
	for _, cOID := range controllerOIDs {
		uiCache.egressPayloadsMu.Lock()
		payload := uiCache.egressPayloads[cOID]
		if err := uiCache.nc.Publish(fmt.Sprintf("egress.ui.%s", clientID), payload.ToBytes()); err != nil {
			lg.GetLogger().Errorf(context.TODO(), "publishCachedEgress error: %v", err)
		}
	}
	uiCache.egressPayloadsMu.Unlock()
}

func Get(hash string) *CacheEntry {
	if !Enabled() {
		return nil
	}
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

	// add "no_cache":true to request for disable cache
	if payload.GetByPath("no_cache").AsBoolDefault(false) {
		return false
	}

	return payload.PathExists("viewer") || payload.PathExists("controllers")
}

func Hash(payload *easyjson.JSON) string {
	clone := payload.Clone().GetPtr()
	//clone.Normalize() everytime in order (by frontend)
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

	if payload.PathExists("payload.command") {
		return
	}

	collectEgress(&payload)
}

func LinkTraceIDAndControllerOID(traceID, controllerOID string) {
	if !Enabled() {
		return
	}
	uiCache.correlatorMu.Lock()
	defer uiCache.correlatorMu.Unlock()
	if v, ok := uiCache.correlator[traceID]; ok {
		v.ControllerOIDs = append(v.ControllerOIDs, controllerOID)
		uiCache.correlator[traceID] = v
	}
}
