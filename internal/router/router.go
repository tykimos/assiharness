package router

import (
	"sort"

	"github.com/tykimos/assiharness/internal/models"
)

// Router matches incoming events to route rules.
type Router struct {
	routes []models.RouteRule
}

// NewRouter creates a Router with routes sorted by priority.
// Lower priority value means higher precedence. Routes with priority 0 default to 100.
func NewRouter(routes []models.RouteRule) *Router {
	normalized := make([]models.RouteRule, len(routes))
	copy(normalized, routes)

	for i := range normalized {
		if normalized[i].Priority == 0 {
			normalized[i].Priority = 100
		}
	}

	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Priority < normalized[j].Priority
	})

	return &Router{routes: normalized}
}

// Match returns the first route that matches the event.
// Routes are evaluated in priority order (lowest value first).
func (r *Router) Match(event models.Event) (agentID string, input map[string]any, matched bool) {
	for _, route := range r.routes {
		if !matchRoute(route, event) {
			continue
		}
		return route.Dispatch.Agent, route.Dispatch.Input, true
	}
	return "", nil, false
}

// matchRoute returns true when all conditions in the route's When clause are satisfied.
func matchRoute(route models.RouteRule, event models.Event) bool {
	when := route.When

	// source must match exactly
	if when.Source != event.Source {
		return false
	}

	// labels: AND condition — event must contain ALL route labels
	if len(when.Labels) > 0 {
		labelSet := make(map[string]struct{}, len(event.Labels))
		for _, l := range event.Labels {
			labelSet[l] = struct{}{}
		}
		for _, required := range when.Labels {
			if _, ok := labelSet[required]; !ok {
				return false
			}
		}
	}

	// status: if non-empty, must match exactly
	if when.Status != "" && when.Status != event.Status {
		return false
	}

	// job: if non-empty, must match event.Payload["job"]
	if when.Job != "" {
		job, _ := event.Payload["job"].(string)
		if when.Job != job {
			return false
		}
	}

	return true
}
