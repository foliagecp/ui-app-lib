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
	pending          sync.Map                   // hash -> PendingEntry (temporary)
	correlator       map[string]CorrelatorEntry // traceID -> hash, controllerOID (temporary)
	correlatorMu     sync.RWMutex               // correlator mutex
	egressPayloads   map[string]*easyjson.JSON  // controllerOID -> egress Payloads
	egressPayloadsMu sync.RWMutex               // egress Payloads mutex
	config           *Config                    // cache config
	subscription     *nats.Subscription         // egress subscription
	nc               *nats.Conn                 // nats connection
	stopCh           chan struct{}              // for gracefully shutdown
}

type CacheEntry struct {
	ExpiresAt      time.Time
	ControllerOIDs []string
}

type PendingEntry struct {
	TraceID        string
	Hash           string
	ControllerOIDs []string
	FirstEgressAt  time.Time
	Timer          *time.Timer
	Mutex          sync.Mutex
}

type CorrelatorEntry struct {
	Hash string
}

func PrepareCollection(ctx *sf.StatefunContextProcessor, hash string) bool {
	if !Enabled() {
		return false
	}

	traceID := ctx.TraceID()

	uiCache.correlatorMu.Lock()
	uiCache.correlator[traceID] = CorrelatorEntry{
		Hash: hash,
	}
	uiCache.correlatorMu.Unlock()

	entry := &PendingEntry{
		TraceID:        traceID,
		Hash:           hash,
		ControllerOIDs: make([]string, 0),
	}

	entry.Timer = time.AfterFunc(
		time.Duration(uiCache.config.CollectTimeoutMS)*time.Millisecond,
		func() { saveToCache(hash) },
	)

	_, loaded := uiCache.pending.LoadOrStore(hash, entry)

	if loaded {
		if !entry.Timer.Stop() {
			<-entry.Timer.C
		}
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
	defer entry.Mutex.Unlock()

	if entry.Timer == nil {
		return
	}

	traceID := entry.TraceID
	entry.Timer = nil

	if len(entry.ControllerOIDs) == 0 {
		uiCache.correlatorMu.Lock()
		delete(uiCache.correlator, traceID)
		uiCache.correlatorMu.Unlock()
		uiCache.pending.Delete(hash)
		return
	}

	uiCache.cache.Store(hash, &CacheEntry{
		ControllerOIDs: entry.ControllerOIDs,
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

	clone := easyjson.NewJSONObject()
	clone.SetByPath("payload", payload.GetByPath("payload"))
	clone.SetByPath("cached", easyjson.NewJSON(true))

	uiCache.correlatorMu.RLock()
	correlatorEntry, inCorrelator := uiCache.correlator[traceId]
	uiCache.correlatorMu.RUnlock()

	uiCache.egressPayloadsMu.Lock()
	uiCache.egressPayloads[controllerOID] = &clone
	uiCache.egressPayloadsMu.Unlock()

	if !inCorrelator {
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

	pendingEntry.ControllerOIDs = append(pendingEntry.ControllerOIDs, controllerOID)
}

func PublishCachedEgress(clientID string, controllerOIDs []string) {
	if !Enabled() {
		return
	}
	for _, cOID := range controllerOIDs {
		uiCache.egressPayloadsMu.RLock()
		payload, exist := uiCache.egressPayloads[cOID]
		uiCache.egressPayloadsMu.RUnlock()
		if !exist || payload == nil {
			lg.GetLogger().Warnf(context.TODO(), "payload not found for controllerOID: %s", cOID)
			continue
		}
		if err := uiCache.nc.Publish(fmt.Sprintf("egress.ui.%s", clientID), payload.ToBytes()); err != nil {
			lg.GetLogger().Errorf(context.TODO(), "publishCachedEgress error: %v", err)
		}
	}
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

	uiCache.cache.CompareAndDelete(hash, entryInterface)
	return nil
}

func IsCacheable(payload *easyjson.JSON) bool {
	if !Enabled() {
		return false
	}

	if payload.PathExists("command") {
		return false
	}

	if payload.GetByPath("no_cache").AsBoolDefault(false) {
		return false
	}

	return payload.PathExists("viewer") || payload.PathExists("controllers")
}

func Hash(payload *easyjson.JSON) string {
	bytes := payload.ToBytes()
	h := fnv.New64a()
	system.MsgOnErrorReturn(h.Write(bytes))
	return strconv.FormatUint(h.Sum64(), 16)
}

func (c *Cache) handleNatsMessage(msg *nats.Msg) {
	payload, ok := easyjson.JSONFromBytes(msg.Data)
	if !ok {
		lg.GetLogger().Errorf(context.TODO(), "invalid nats message")
		return
	}

	if payload.PathExists("payload.command") {
		return
	}

	collectEgress(&payload)
}
