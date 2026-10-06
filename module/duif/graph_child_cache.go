package duif

import (
	"sync"
	"time"
)

// De korte cache op de afgebakende kinderlijst van één ouder.
//
// De lijst achter de klik is exact doordat élk kandidaat-kind werkelijk gelezen
// wordt: pas dan weten total, truncated en nextOffset wat de lezer krijgt.
// Zonder cache stond die ronde per verzoek opnieuw in children, en de leesseam
// (graphNodeReader) kent geen batch-read — één GetNode per id. Een ouder met N
// kinderen kostte een bladertocht daardoor N reads pér pagina: 900 standpunten
// gingen van 908 naar 6314 reads over zeven pagina's.
//
// De cache bewaart wat die ronde opleverde: de afgebakende kinderlijst, in de
// vaste volgorde waarop de paginering leunt. Een bladertocht betaalt de
// volledige ronde daarmee één keer; de pagina's erna lezen alleen nog hun eigen
// nodes.
const (
	// graphChildCacheTTL is de staleness-grens. Binnen dit venster kan de
	// telling achterlopen op een schrijver buiten de import om; de graaf kent
	// er geen (hij is read-only en de importer is de enige schrijver), dus in
	// de praktijk is de grens de import — en die leegt de cache zelf.
	graphChildCacheTTL = 60 * time.Second
	// graphChildCacheEntries begrenst het geheugen: hooguit dit aantal ouders
	// tegelijk. Eén regel is een id plus een relatie per kind, geen node.
	graphChildCacheEntries = 256
)

type graphChildCacheEntry struct {
	// children is gedeeld met elke lezer die hem uit de cache haalt. Hij wordt
	// nooit gemuteerd of aangevuld: children snijdt er alleen een pagina uit.
	children []graphChildEdge
	expires  time.Time
}

// graphChildCache is een kleine cache met vervaltijd. De nulwaarde werkt.
type graphChildCache struct {
	mu      sync.Mutex
	entries map[string]graphChildCacheEntry
	// now is de klok, zodat een test de vervaltijd kan verzetten zonder te
	// wachten.
	now func() time.Time
}

func (c *graphChildCache) klok() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// graphChildCacheKey bindt een lijst aan de kennisbank waarin hij geteld is:
// een andere kennisbank is een andere afbakening en dus een andere lijst.
func graphChildCacheKey(kbID, parentID string) string { return kbID + "\x00" + parentID }

// get geeft de bewaarde lijst, of nil als hij er niet (meer) is.
func (c *graphChildCache) get(key string) ([]graphChildEdge, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if !c.klok().Before(entry.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return entry.children, true
}

// put bewaart een lijst. Zit de cache vol, dan gaan eerst de vervallen regels
// eruit en anders de regel die het eerst zou vervallen — de oudste, want de
// vervaltijd is voor iedereen gelijk.
func (c *graphChildCache) put(key string, children []graphChildEdge) {
	c.mu.Lock()
	defer c.mu.Unlock()
	nu := c.klok()
	if c.entries == nil {
		c.entries = map[string]graphChildCacheEntry{}
	}
	if _, staat := c.entries[key]; !staat && len(c.entries) >= graphChildCacheEntries {
		for k, e := range c.entries {
			if !nu.Before(e.expires) {
				delete(c.entries, k)
			}
		}
	}
	for len(c.entries) >= graphChildCacheEntries {
		oudste, gezet := "", time.Time{}
		for k, e := range c.entries {
			if !gezet.IsZero() && !e.expires.Before(gezet) {
				continue
			}
			oudste, gezet = k, e.expires
		}
		delete(c.entries, oudste)
	}
	c.entries[key] = graphChildCacheEntry{children: children, expires: nu.Add(graphChildCacheTTL)}
}

// drop gooit één lijst weg: children doet dat zodra blijkt dat een kind uit de
// bewaarde lijst er niet meer is, want dan klopt de belofte niet meer.
func (c *graphChildCache) drop(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

// clear leegt de hele cache. De importer roept dit aan als een import klaar is:
// die schrijft of wist juist de rijen waaruit deze lijsten geteld zijn.
func (c *graphChildCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

// invalidateChildCache is de haak die de kennisbank-import aan de graaf hangt.
func (g *graphService) invalidateChildCache() { g.childCache.clear() }
