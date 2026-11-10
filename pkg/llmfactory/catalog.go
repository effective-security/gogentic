package llmfactory

import (
	"context"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/effective-security/gogentic/pkg/llms"
	"github.com/effective-security/gogentic/pkg/metricskey"
	"github.com/effective-security/xlog"
)

const (
	// defaultDiscoveryRefresh is how long a model list is trusted when the
	// configuration does not say.
	defaultDiscoveryRefresh = time.Hour
	// minDiscoveryRefresh is the shortest refresh interval accepted.
	minDiscoveryRefresh = time.Minute
	// modelListTimeout bounds one fetch of a model list.
	modelListTimeout = 10 * time.Second
	// modelListRetryInterval is the shortest time between two fetches of the
	// same list, so a provider that fails is not asked on every resolution.
	modelListRetryInterval = time.Minute

	// stageLatest marks an ID that follows the newest snapshot of a model.
	stageLatest = "latest"
)

// modelList is one fetched model list of a provider. It is immutable, apart
// from what is derived from it on first use and the record of what was
// already logged about it.
type modelList struct {
	// generation counts the lists fetched for the provider, from 1.
	generation uint64
	// fetched is when the list was read.
	fetched time.Time
	// models holds the entries in the provider's order.
	models []llms.ModelInfo
	// names holds the parsed ID of each entry of models.
	names []llms.ModelName
	// byID maps a model ID to its position in models.
	byID map[string]int
	// usable holds the models of the list the provider policy lets the
	// factory use, computed on first use.
	usable     []string
	usableOnce sync.Once
	// noted records what was logged about this list.
	noted onceSet
}

// newModelList indexes a fetched list.
func newModelList(generation uint64, fetched time.Time, models []llms.ModelInfo) *modelList {
	list := &modelList{
		generation: generation,
		fetched:    fetched,
		models:     models,
		names:      make([]llms.ModelName, 0, len(models)),
		byID:       make(map[string]int, len(models)),
	}
	for i, model := range models {
		list.names = append(list.names, llms.ParseModelName(model.ID))
		if _, dup := list.byID[model.ID]; !dup {
			list.byID[model.ID] = i
		}
	}
	return list
}

// lookup returns the entry that describes the model: the entry with that ID,
// or else the newest snapshot of it. Providers accept a floating ID such as
// claude-haiku-4-5 while listing only the dated claude-haiku-4-5-20251001, so
// an ID without a snapshot is described by a listed snapshot of the same
// model. A nil list describes nothing.
func (l *modelList) lookup(id string) (llms.ModelInfo, bool) {
	if l == nil {
		return llms.ModelInfo{}, false
	}
	if i, ok := l.byID[id]; ok {
		return l.models[i], true
	}
	ref := llms.ParseModelName(id)
	if ref.Snapshot != "" {
		return llms.ModelInfo{}, false
	}
	best := -1
	for i, name := range l.names {
		if !isSnapshotOf(name, ref) {
			continue
		}
		if best < 0 || l.betterSnapshot(i, best) {
			best = i
		}
	}
	if best < 0 {
		return llms.ModelInfo{}, false
	}
	return l.models[best], true
}

// betterSnapshot reports whether entry i is the better description of a
// floating ID than entry j: one still in service, then the newer one.
func (l *modelList) betterSnapshot(i, j int) bool {
	iRetired := l.models[i].Lifecycle == llms.ModelLifecycleRetired
	jRetired := l.models[j].Lifecycle == llms.ModelLifecycleRetired
	if iRetired != jRetired {
		return !iRetired
	}
	return l.names[i].Snapshot > l.names[j].Snapshot
}

// isSnapshotOf reports whether name is a dated snapshot of the floating ID
// ref: the same route, family, variant and version. A ref that ends in
// "latest" follows the generally available snapshots.
func isSnapshotOf(name, ref llms.ModelName) bool {
	if name.Snapshot == "" || name.Key() != ref.Key() {
		return false
	}
	if llms.CompareVersions(name.Version, ref.Version) != 0 {
		return false
	}
	return name.Stage == ref.Stage || (ref.Stage == stageLatest && name.Stage == "")
}

// serves reports whether the provider still answers requests for the model:
// the list describes it and does not mark it retired.
func (l *modelList) serves(id string) bool {
	info, ok := l.lookup(id)
	return ok && info.Lifecycle != llms.ModelLifecycleRetired
}

// lifecycle returns the lifecycle stage the list reports for the model.
func (l *modelList) lifecycle(id string) llms.ModelLifecycle {
	info, _ := l.lookup(id)
	return info.Lifecycle
}

// onceSet remembers keys, so that an event is reported once.
type onceSet struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// first reports whether key was not seen before, and remembers it.
func (s *onceSet) first(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[key]; ok {
		return false
	}
	if s.seen == nil {
		s.seen = make(map[string]struct{})
	}
	s.seen[key] = struct{}{}
	return true
}

// modelFetch is one fetch of a model list. A caller that waits for a fetch
// reads the outcome of that fetch, not of whichever finished last.
type modelFetch struct {
	// done is closed when the fetch has finished.
	done chan struct{}
	// err is the outcome of the fetch. It is set before done is closed and
	// not changed afterwards.
	err error
}

// modelCatalog holds the model list of one provider. The list is fetched on
// first use, then refreshed in the background once it is older than the
// refresh interval. A fetch that fails keeps the last good list.
//
// A catalog has its own lock and never calls the provider while holding it,
// so it can be used without the factory lock.
type modelCatalog struct {
	// provider is the configured provider name, for logs and metrics.
	provider string
	// refresh is how long a list is trusted.
	refresh time.Duration
	// open builds the lister on the first fetch.
	open func() (llms.ModelLister, error)
	// now is the clock; tests replace it.
	now func() time.Time

	// lister is used by the fetch in flight only.
	lister llms.ModelLister

	mu sync.Mutex
	// list is the last good list, or nil before the first one.
	list *modelList
	// attempted is when the last fetch started.
	attempted time.Time
	// inflight is the fetch in flight; nil when there is none.
	inflight *modelFetch
}

// newModelCatalog returns an empty catalog. Nothing is fetched until the list
// is asked for.
func newModelCatalog(provider string, refresh time.Duration, open func() (llms.ModelLister, error)) *modelCatalog {
	return &modelCatalog{
		provider: provider,
		refresh:  refresh,
		open:     open,
		now:      time.Now,
	}
}

// current returns the list to resolve with, or nil when there is none.
//
// Without a list it fetches one and waits for it, for at most the fetch
// timeout or until ctx is done. With a list older than the refresh interval
// it starts a fetch in the background and returns the list it has. After a
// fetch, another one is not started for modelListRetryInterval, so a failing
// provider costs one wait rather than one per call.
func (c *modelCatalog) current(ctx context.Context) *modelList {
	c.mu.Lock()
	list := c.list
	var fetch *modelFetch
	if list == nil || c.now().Sub(list.fetched) >= c.refresh {
		fetch = c.startLocked(ctx, false)
	}
	c.mu.Unlock()

	if list != nil || fetch == nil {
		return list
	}
	select {
	case <-fetch.done:
	case <-ctx.Done():
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.list
}

// refreshNow fetches the list, waits for it, and returns the error of that
// fetch: the one it started, or the one already in flight that it joined. The
// list in use is replaced only when the fetch succeeds.
func (c *modelCatalog) refreshNow(ctx context.Context) error {
	c.mu.Lock()
	fetch := c.startLocked(ctx, true)
	c.mu.Unlock()

	select {
	case <-fetch.done:
	case <-ctx.Done():
		return errors.Wrapf(ctx.Err(), "provider %s: refresh model list", c.provider)
	}
	return fetch.err
}

// startLocked starts a fetch unless one is in flight, and returns the fetch
// to wait for. It returns nil when the last fetch is too recent to try again,
// unless force is set. The caller holds c.mu.
func (c *modelCatalog) startLocked(ctx context.Context, force bool) *modelFetch {
	if c.inflight != nil {
		return c.inflight
	}
	now := c.now()
	if !force && !c.attempted.IsZero() && now.Sub(c.attempted) < modelListRetryInterval {
		return nil
	}
	c.attempted = now
	fetch := &modelFetch{
		done: make(chan struct{}),
	}
	c.inflight = fetch
	// The fetch outlives the call that started it: other calls wait for it,
	// and a refresh runs after its caller returned.
	go c.fetch(context.WithoutCancel(ctx), fetch)
	return fetch
}

// fetch reads the list once and publishes the outcome. Only one fetch runs at
// a time, so it alone uses c.lister.
func (c *modelCatalog) fetch(ctx context.Context, fetch *modelFetch) {
	ctx, cancel := context.WithTimeout(ctx, modelListTimeout)
	defer cancel()
	models, err := c.load(ctx)

	c.mu.Lock()
	previous := c.list
	if err == nil {
		generation := uint64(1)
		if previous != nil {
			generation = previous.generation + 1
		}
		c.list = newModelList(generation, c.now(), models)
	}
	c.inflight = nil
	c.mu.Unlock()
	// Waiters read the outcome after done is closed.
	fetch.err = err
	close(fetch.done)

	if err != nil {
		metricskey.StatsLLMModelListFailed.IncrCounter(1, c.provider)
		logger.KV(xlog.ERROR,
			"reason", "model_list_failed",
			"provider", c.provider,
			"stale", previous != nil,
			"err", err.Error(),
		)
		return
	}
	logger.KV(xlog.INFO,
		"status", "model_list_loaded",
		"provider", c.provider,
		"models", len(models),
	)
}

// load asks the provider for its models. An empty list is an error: no
// provider serves no model, and resolving against an empty list would make
// every configured model look retired.
func (c *modelCatalog) load(ctx context.Context) ([]llms.ModelInfo, error) {
	if c.lister == nil {
		lister, err := c.open()
		if err != nil {
			return nil, errors.Wrapf(err, "provider %s: create model lister", c.provider)
		}
		c.lister = lister
	}
	models, err := c.lister.ListModels(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "provider %s: list models", c.provider)
	}
	if len(models) == 0 {
		return nil, errors.Errorf("provider %s: model list is empty", c.provider)
	}
	return models, nil
}
