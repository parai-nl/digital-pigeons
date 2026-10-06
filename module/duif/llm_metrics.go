package duif

import (
	"context"
	"time"
)

// De diagnose achter trage beurten: modelcalls via de proxy duren onvoorspelbaar lang —
// hetzelfde verzoek is de ene keer in twee seconden klaar en de andere keer pas
// na een minuut. Dat lossen wij hier niet op, maar het moet wél meetbaar zijn
// zonder ook maar één woord van de prompt of het antwoord te bewaren. Daarom
// draagt elke modelcall twee dingen mee: welke route hem stelde, en hoe lang
// hij duurde.
//
// De route staat in de context in plaats van in de request, omdat de
// aanroepende laag hem kent en de begrenzer (boundedLLM) de enige plek is waar
// álle calls langskomen. Een call zonder route is geen fout: dan meten we hem
// als "unknown" in plaats van hem stil te laten vallen.
const (
	// routeFacilitator is de gespreksleiding zelf: de tool-lus die besluit
	// wat er moet gebeuren.
	routeFacilitator = "facilitator"
	// routeMediation is haar afsluitende beurt zonder tools.
	routeMediation = "mediation"
	// routePigeon is het antwoord van de duif in de chat — de call waar de
	// lezer op wacht.
	routePigeon = "pigeon"
	// routeAudit is de bronaudit. Geen live beurt stelt hem meer.
	routeAudit = "audit"
	// routeCandidate is de kandidaatgenerator van de zelfstandige duif.
	routeCandidate = "candidate"
	// routeMeeting is de spreekbeurt aan de vergadertafel.
	routeMeeting = "meeting"
	// routeMeetingQuery en routeCandidateQuery zijn de korte
	// herformuleringscall vóór de retrieval, per vergaderroute apart. Apart,
	// omdat de twee routes verschillende eisen stellen: de knop mag de lezer
	// seconden kosten, de zelfstandige route hoort onzichtbaar te blijven. Zo
	// is per route af te lezen wat de stap werkelijk kost.
	routeMeetingQuery   = "meeting_query"
	routeCandidateQuery = "candidate_query"
	// routeUnknown dekt een call van een pad dat zijn route niet zette.
	routeUnknown = "unknown"
	// phaseGrounding is de fase tussen "de duif krijgt het woord" en haar
	// eerste token: het opzoeken in de kennisbank. Hij is geen modelroute —
	// er gaat geen taalmodel aan te pas — maar wel een fase waarin de lezer
	// kan wegvallen, en dus een waarde die in het log moet kunnen staan.
	phaseGrounding = "pigeon_grounding"
	// phaseRelated is de brede zoekactie die ná een kennisgat nog naar
	// grenzend onderzoek kijkt. Hij staat los van phaseGrounding
	// omdat hij een eigen wachttijd heeft en een eigen zin op het scherm
	// verdient ("Verwante bronnen zoeken…"); ook hier komt geen
	// taalmodel aan te pas.
	phaseRelated = "pigeon_related_search"
)

// metricLLMDuration is de duur van één modelcall in milliseconden, met
// dimensies `model` en `route`. Bewust alleen een getal: er gaat geen tekst,
// geen sessie-id en geen gebruiker in mee.
const metricLLMDuration = "llm.call.duration_ms"

type llmRouteKey struct{}

// withLLMRoute merkt de context van één modelcall met zijn route.
func withLLMRoute(ctx context.Context, route string) context.Context {
	return context.WithValue(ctx, llmRouteKey{}, route)
}

// llmRouteFrom leest de route terug; zonder markering "unknown".
func llmRouteFrom(ctx context.Context) string {
	if route, ok := ctx.Value(llmRouteKey{}).(string); ok && route != "" {
		return route
	}
	return routeUnknown
}

// trackLLMDuration schrijft de duurmetriek weg. Zonder tracker gebeurt er
// niets: unit-tests en injectie-adapters wiren er geen.
func (b *boundedLLM) trackLLMDuration(ctx context.Context, model string, started time.Time) {
	if b.tracker == nil {
		return
	}
	if model == "" {
		model = "unset"
	}
	b.tracker.Track(ctx, metricLLMDuration,
		map[string]string{"model": model, "route": llmRouteFrom(ctx)},
		float64(time.Since(started).Milliseconds()))
}
