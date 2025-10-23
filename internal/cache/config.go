package cache

import (
	"context"

	"github.com/foliagecp/sdk/statefun"
	lg "github.com/foliagecp/sdk/statefun/logger"
	"github.com/foliagecp/sdk/statefun/system"
	"github.com/nats-io/nats.go"
)

var config *Config

type Config struct {
	Enabled              bool
	TTLSeconds           int
	CollectTimeoutMS     int
	CorrelatorTTLSeconds int
	MaxEntries           int
}

func InitConfig() {
	config = &Config{
		Enabled:              system.GetEnvMustProceed("UI_APP_LIB_CACHE_ENABLED", true),
		TTLSeconds:           system.GetEnvMustProceed("UI_APP_LIB_CACHE_TTL_SECONDS", 600),
		CollectTimeoutMS:     system.GetEnvMustProceed("UI_APP_LIB_CACHE_COLLECT_TIMEOUT_MS", 5000),
		CorrelatorTTLSeconds: system.GetEnvMustProceed("UI_APP_LIB_CACHE_CORRELATOR_TTL_SECONDS", 120),
		MaxEntries:           system.GetEnvMustProceed("UI_APP_LIB_CACHE_MAX_ENTRIES", 10000),
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
		correlator: make(map[string]string),
		config:     config,
		nc:         nc,
	}

	sub, err := nc.Subscribe("egress.ui.>", func(msg *nats.Msg) {
		cache.handleNatsMessage(msg)
	})
	if err != nil {
		le.Errorf(context.TODO(), "Failed to subscribe to egress.ui.>: %s", err.Error())
		return
	}

	cache.subscription = sub

	uiCache = cache //global ui cache

	return
}

func Enabled() bool {
	return config != nil && config.Enabled
}
