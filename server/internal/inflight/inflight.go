package inflight

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const defaultTerminalRetention = 3 * time.Second

type Phase string

const (
	PhaseAccepted   Phase = "accepted"
	PhaseRouted     Phase = "routed"
	PhaseResponding Phase = "responding"
	PhaseCompleted  Phase = "completed"
	PhaseFailed     Phase = "failed"
	PhaseCancelled  Phase = "cancelled"
)

type Request struct {
	RequestID     string    `json:"request_id"`
	APIKeyID      string    `json:"api_key_id"`
	APIKeyName    string    `json:"api_key_name"`
	ModelKey      string    `json:"model_key"`
	ModelProvider string    `json:"model_provider"`
	SiteID        string    `json:"upstream_site_id,omitempty"`
	SiteName      string    `json:"upstream_site_name,omitempty"`
	SiteType      string    `json:"upstream_site_type,omitempty"`
	Attempt       int       `json:"attempt"`
	Stream        bool      `json:"stream"`
	Phase         Phase     `json:"phase"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Route struct {
	SiteID   string
	SiteName string
	SiteType string
	Attempt  int
}

type TokenAmount struct {
	Total  int64
	Input  int64
	Output int64
	Cached int64
}

func (amount TokenAmount) normalized() TokenAmount {
	if amount.Input < 0 {
		amount.Input = 0
	}
	if amount.Output < 0 {
		amount.Output = 0
	}
	if amount.Cached < 0 {
		amount.Cached = 0
	}
	if amount.Total <= 0 {
		amount.Total = amount.Input + amount.Output
	}
	return amount
}

type UsageTotal struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	TotalTokens int64  `json:"total_tokens"`
}

type UsageCell struct {
	APIKeyID      string `json:"api_key_id"`
	APIKeyName    string `json:"api_key_name"`
	SiteID        string `json:"site_id"`
	SiteName      string `json:"site_name"`
	ModelKey      string `json:"model_key"`
	ModelProvider string `json:"model_provider"`
	TotalTokens   int64  `json:"total_tokens"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	CachedTokens  int64  `json:"cached_tokens"`
}

type usageCellKey struct {
	APIKeyID string
	SiteID   string
	ModelKey string
}

type Event struct {
	Sequence        uint64      `json:"sequence"`
	Type            string      `json:"type"`
	Request         *Request    `json:"request,omitempty"`
	RequestID       string      `json:"request_id,omitempty"`
	Tokens          int64       `json:"tokens,omitempty"`
	TotalTokens     int64       `json:"total_tokens,omitempty"`
	DownstreamUsage *UsageTotal `json:"downstream_usage,omitempty"`
	UpstreamUsage   *UsageTotal `json:"upstream_usage,omitempty"`
	UsageCell       *UsageCell  `json:"usage_cell,omitempty"`
}

type Snapshot struct {
	Sequence        uint64       `json:"sequence"`
	Requests        []Request    `json:"requests"`
	TotalTokens     int64        `json:"total_tokens"`
	DownstreamUsage []UsageTotal `json:"downstream_usage"`
	UpstreamUsage   []UsageTotal `json:"upstream_usage"`
	UsageCells      []UsageCell  `json:"usage_cells"`
}

type Registry struct {
	mu                sync.RWMutex
	requests          map[string]Request
	subscribers       map[uint64]chan Event
	nextID            uint64
	sequence          uint64
	totalTokens       int64
	downstreamUsage   map[string]UsageTotal
	upstreamUsage     map[string]UsageTotal
	usageCells        map[usageCellKey]UsageCell
	terminalRetention time.Duration
}

func NewRegistry() *Registry {
	return &Registry{
		requests:          map[string]Request{},
		subscribers:       map[uint64]chan Event{},
		downstreamUsage:   map[string]UsageTotal{},
		upstreamUsage:     map[string]UsageTotal{},
		usageCells:        map[usageCellKey]UsageCell{},
		terminalRetention: defaultTerminalRetention,
	}
}

func (r *Registry) Start(request Request) {
	if r == nil || request.RequestID == "" {
		return
	}
	now := time.Now()
	request.Phase = PhaseAccepted
	request.StartedAt = now
	request.UpdatedAt = now

	r.mu.Lock()
	if r.requests == nil {
		r.requests = map[string]Request{}
	}
	if r.subscribers == nil {
		r.subscribers = map[uint64]chan Event{}
	}
	r.requests[request.RequestID] = request
	event := r.newEventLocked("upsert", &request, "")
	r.mu.Unlock()
	r.publish(event)
}

func (r *Registry) Route(requestID string, route Route) {
	r.update(requestID, func(request *Request) {
		request.SiteID = route.SiteID
		request.SiteName = route.SiteName
		request.SiteType = route.SiteType
		request.Attempt = route.Attempt
		request.Phase = PhaseRouted
	})
}

func (r *Registry) Model(requestID string, modelKey string, provider string) {
	r.update(requestID, func(request *Request) {
		request.ModelKey = modelKey
		request.ModelProvider = provider
	})
}

func (r *Registry) Responding(requestID string) {
	r.update(requestID, func(request *Request) {
		request.Phase = PhaseResponding
	})
}

func (r *Registry) Finish(requestID string, phase Phase) {
	if r == nil || requestID == "" {
		return
	}
	if !isTerminalPhase(phase) {
		phase = PhaseFailed
	}

	r.mu.Lock()
	request, ok := r.requests[requestID]
	if !ok || isTerminalPhase(request.Phase) {
		r.mu.Unlock()
		return
	}
	request.Phase = phase
	request.UpdatedAt = time.Now()
	r.requests[requestID] = request
	event := r.newEventLocked("upsert", &request, "")
	r.mu.Unlock()
	r.publish(event)

	retention := r.terminalRetention
	if retention <= 0 {
		retention = defaultTerminalRetention
	}
	time.AfterFunc(retention, func() {
		r.remove(requestID, request.UpdatedAt)
	})
}

func (r *Registry) AddTokens(requestID string, amount TokenAmount) {
	if r == nil {
		return
	}
	amount = amount.normalized()
	if amount.Total <= 0 {
		return
	}
	r.mu.Lock()
	r.totalTokens += amount.Total
	request, ok := r.requests[requestID]
	var downstreamUsage *UsageTotal
	var upstreamUsage *UsageTotal
	var usageCell *UsageCell
	if ok && request.APIKeyID != "" {
		if r.downstreamUsage == nil {
			r.downstreamUsage = map[string]UsageTotal{}
		}
		usage := r.downstreamUsage[request.APIKeyID]
		usage.ID = request.APIKeyID
		usage.Name = request.APIKeyName
		usage.TotalTokens += amount.Total
		r.downstreamUsage[request.APIKeyID] = usage
		current := usage
		downstreamUsage = &current

		if r.usageCells == nil {
			r.usageCells = map[usageCellKey]UsageCell{}
		}
		key := usageCellKey{APIKeyID: request.APIKeyID, SiteID: request.SiteID, ModelKey: request.ModelKey}
		cell := r.usageCells[key]
		cell.APIKeyID = request.APIKeyID
		cell.APIKeyName = request.APIKeyName
		cell.SiteID = request.SiteID
		cell.SiteName = request.SiteName
		cell.ModelKey = request.ModelKey
		cell.ModelProvider = request.ModelProvider
		cell.TotalTokens += amount.Total
		cell.InputTokens += amount.Input
		cell.OutputTokens += amount.Output
		cell.CachedTokens += amount.Cached
		r.usageCells[key] = cell
		currentCell := cell
		usageCell = &currentCell
	}
	if ok && request.SiteID != "" {
		if r.upstreamUsage == nil {
			r.upstreamUsage = map[string]UsageTotal{}
		}
		usage := r.upstreamUsage[request.SiteID]
		usage.ID = request.SiteID
		usage.Name = request.SiteName
		usage.TotalTokens += amount.Total
		r.upstreamUsage[request.SiteID] = usage
		current := usage
		upstreamUsage = &current
	}
	event := r.newEventLocked("usage", nil, requestID)
	event.Tokens = amount.Total
	event.TotalTokens = r.totalTokens
	event.DownstreamUsage = downstreamUsage
	event.UpstreamUsage = upstreamUsage
	event.UsageCell = usageCell
	r.mu.Unlock()
	r.publish(event)
}

func (r *Registry) Snapshot() Snapshot {
	if r == nil {
		return Snapshot{Requests: []Request{}}
	}
	r.mu.RLock()
	requests := make([]Request, 0, len(r.requests))
	for _, request := range r.requests {
		requests = append(requests, request)
	}
	sequence := r.sequence
	totalTokens := r.totalTokens
	downstreamUsage := usageTotals(r.downstreamUsage)
	upstreamUsage := usageTotals(r.upstreamUsage)
	usageCells := usageCells(r.usageCells)
	r.mu.RUnlock()

	sort.Slice(requests, func(i, j int) bool {
		if requests[i].StartedAt.Equal(requests[j].StartedAt) {
			return requests[i].RequestID < requests[j].RequestID
		}
		return requests[i].StartedAt.Before(requests[j].StartedAt)
	})
	return Snapshot{Sequence: sequence, Requests: requests, TotalTokens: totalTokens, DownstreamUsage: downstreamUsage, UpstreamUsage: upstreamUsage, UsageCells: usageCells}
}

func usageTotals(items map[string]UsageTotal) []UsageTotal {
	totals := make([]UsageTotal, 0, len(items))
	for _, item := range items {
		totals = append(totals, item)
	}
	sort.Slice(totals, func(i, j int) bool {
		if totals[i].TotalTokens != totals[j].TotalTokens {
			return totals[i].TotalTokens > totals[j].TotalTokens
		}
		return totals[i].Name < totals[j].Name
	})
	return totals
}

func usageCells(items map[usageCellKey]UsageCell) []UsageCell {
	cells := make([]UsageCell, 0, len(items))
	for _, item := range items {
		cells = append(cells, item)
	}
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].TotalTokens != cells[j].TotalTokens {
			return cells[i].TotalTokens > cells[j].TotalTokens
		}
		if cells[i].APIKeyName != cells[j].APIKeyName {
			return cells[i].APIKeyName < cells[j].APIKeyName
		}
		if cells[i].SiteName != cells[j].SiteName {
			return cells[i].SiteName < cells[j].SiteName
		}
		return cells[i].ModelKey < cells[j].ModelKey
	})
	return cells
}

func (r *Registry) Subscribe() (<-chan Event, func()) {
	if r == nil {
		closed := make(chan Event)
		close(closed)
		return closed, func() {}
	}
	r.mu.Lock()
	if r.subscribers == nil {
		r.subscribers = map[uint64]chan Event{}
	}
	id := r.nextID
	r.nextID++
	events := make(chan Event, 128)
	r.subscribers[id] = events
	r.mu.Unlock()

	return events, func() {
		r.mu.Lock()
		if current, ok := r.subscribers[id]; ok {
			delete(r.subscribers, id)
			close(current)
		}
		r.mu.Unlock()
	}
}

func (r *Registry) update(requestID string, change func(*Request)) {
	if r == nil || requestID == "" {
		return
	}
	r.mu.Lock()
	request, ok := r.requests[requestID]
	if !ok || isTerminalPhase(request.Phase) {
		r.mu.Unlock()
		return
	}
	change(&request)
	request.UpdatedAt = time.Now()
	r.requests[requestID] = request
	event := r.newEventLocked("upsert", &request, "")
	r.mu.Unlock()
	r.publish(event)
}

func (r *Registry) remove(requestID string, finishedAt time.Time) {
	r.mu.Lock()
	request, ok := r.requests[requestID]
	if !ok || !request.UpdatedAt.Equal(finishedAt) || !isTerminalPhase(request.Phase) {
		r.mu.Unlock()
		return
	}
	delete(r.requests, requestID)
	event := r.newEventLocked("remove", nil, requestID)
	r.mu.Unlock()
	r.publish(event)
}

func (r *Registry) newEventLocked(eventType string, request *Request, requestID string) Event {
	r.sequence++
	if request != nil {
		requestID = request.RequestID
	}
	return Event{Sequence: r.sequence, Type: eventType, Request: request, RequestID: requestID}
}

func (r *Registry) publish(event Event) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, events := range r.subscribers {
		select {
		case events <- event:
		default:
		}
	}
}

func isTerminalPhase(phase Phase) bool {
	return phase == PhaseCompleted || phase == PhaseFailed || phase == PhaseCancelled
}

var current atomic.Int64
var defaultRegistry = NewRegistry()

func Enter() func() {
	current.Add(1)
	var once sync.Once
	return func() {
		once.Do(func() {
			current.Add(-1)
		})
	}
}

func Count() int64 {
	return current.Load()
}

func Start(request Request) {
	defaultRegistry.Start(request)
}

func RouteRequest(requestID string, route Route) {
	defaultRegistry.Route(requestID, route)
}

func SetModel(requestID string, modelKey string, provider string) {
	defaultRegistry.Model(requestID, modelKey, provider)
}

func MarkResponding(requestID string) {
	defaultRegistry.Responding(requestID)
}

func Finish(requestID string, phase Phase) {
	defaultRegistry.Finish(requestID, phase)
}

func AddTokens(requestID string, amount TokenAmount) {
	defaultRegistry.AddTokens(requestID, amount)
}

func CurrentSnapshot() Snapshot {
	return defaultRegistry.Snapshot()
}

func Subscribe() (<-chan Event, func()) {
	return defaultRegistry.Subscribe()
}
