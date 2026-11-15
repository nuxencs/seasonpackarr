// Copyright (c) 2023 - 2026, nuxen and the seasonpackarr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package seasonpack

import (
	"time"

	"github.com/nuxencs/seasonpackarr/internal/domain"
	"github.com/nuxencs/seasonpackarr/internal/torrentclient"
	"github.com/nuxencs/seasonpackarr/internal/torrents"

	"github.com/puzpuzpuz/xsync/v3"
)

// importPlanCacheTTL is how long the import can use the plan of a match.
const importPlanCacheTTL = 2 * time.Minute

// planCache carries the import plan of an accepted match to the import.
type planCache struct {
	plans *xsync.MapOf[importPlanCacheKey, cachedImportPlan]
}

func newPlanCache() planCache {
	return planCache{plans: xsync.NewMapOf[importPlanCacheKey, cachedImportPlan]()}
}

type importPlanCacheKey struct {
	clientName string
	hashes     torrents.Hashes
}

// planScope is what a stored plan is valid for, with its torrent identity.
type planScope struct {
	clientName    string
	releaseName   string
	clientConfig  domain.Client
	fuzzyMatching domain.FuzzyMatching
}

type cachedImportPlan struct {
	plan      importPlan
	scope     planScope
	expiresAt time.Time
}

func (c *call) planScope() planScope {
	return planScope{
		clientName:    c.client.Name(),
		releaseName:   c.release,
		clientConfig:  c.clientConfig(),
		fuzzyMatching: c.cfg.FuzzyMatching,
	}
}

func (s planScope) equal(other planScope) bool {
	return s.clientName == other.clientName &&
		s.releaseName == other.releaseName &&
		s.fuzzyMatching == other.fuzzyMatching &&
		s.clientConfig.Equal(other.clientConfig)
}

// store keeps plan for scope, and removes each expired plan.
func (pc planCache) store(scope planScope, plan importPlan) {
	now := time.Now()
	pc.plans.Range(func(key importPlanCacheKey, cached cachedImportPlan) bool {
		if now.After(cached.expiresAt) {
			pc.plans.Delete(key)
		}
		return true
	})
	scope.clientConfig = scope.clientConfig.Clone()
	pc.plans.Store(importPlanCacheKey{clientName: scope.clientName, hashes: plan.hashes}, cachedImportPlan{
		plan:      plan,
		scope:     scope,
		expiresAt: now.Add(importPlanCacheTTL),
	})
}

// load returns the plan of the torrent while it is valid for scope. It removes
// a plan that is expired or that was stored for another scope.
func (pc planCache) load(scope planScope, hashes torrents.Hashes) (importPlan, bool) {
	key := importPlanCacheKey{clientName: scope.clientName, hashes: hashes}
	cached, ok := pc.plans.Load(key)
	if !ok {
		return importPlan{}, false
	}
	if time.Now().After(cached.expiresAt) || !cached.scope.equal(scope) {
		pc.plans.Delete(key)
		return importPlan{}, false
	}
	return cached.plan, true
}

func (pc planCache) drop(clientName string, hashes torrents.Hashes) {
	pc.plans.Delete(importPlanCacheKey{clientName: clientName, hashes: hashes})
}

// dropEndpoint removes the plans of each client on endpoint.
func (pc planCache) dropEndpoint(endpoint torrentclient.Endpoint) {
	pc.plans.Range(func(key importPlanCacheKey, cached cachedImportPlan) bool {
		if torrentclient.EndpointOf(cached.scope.clientConfig) == endpoint {
			pc.plans.Delete(key)
		}
		return true
	})
}
