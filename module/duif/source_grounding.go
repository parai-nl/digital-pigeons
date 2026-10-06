package duif

import (
	"context"
	"errors"
	"strings"

	"github.com/parai-nl/parai-core/data"
	"github.com/parai-nl/parai-core/pkg/interaction"
)

// Core's typed Quote deliberately omits corpus-specific properties. Its public
// read-only GetNode API preserves the distinction between false and absent.
type quoteMetadataReader interface {
	GetNode(context.Context, data.NodeID) (map[string]any, error)
}

const (
	groundingVerified   = "corpus_grounded"
	groundingUnverified = "unverified"
	groundingRejected   = "rejected"
	// groundingUnreadable is geen oordeel over de bron maar een storing: de
	// grondingsmetadata was niet te lezen. Hij haalt de wire nooit — de gate
	// laat zo'n kandidaat vallen als dropEvidenceUnreadable — en staat los van
	// groundingRejected, want dáár is de controle wél gedaan.
	groundingUnreadable = "unreadable"
	maxGroundingQuotes  = 8
)

var errGroundingUnavailable = errors.New("source grounding metadata unavailable")

// filterGrounding never converts an explicit false flag into a checked quote.
// Missing legacy flags/capability remain usable but explicitly unverified. They
// are not evidence of an extraction error. A failed metadata read is an actual
// verification outage: dat is groundingUnreadable, en de gate laat die kandidaat
// vallen. Een aantoonbaar afgekeurde gronding (groundingRejected) is iets
// anders: die bron blijft staan, zónder haar passages.
func (r *retrieval) filterGrounding(ctx context.Context, src interaction.RAGSource, rec *searchRecorder) (interaction.RAGSource, string) {
	if src.Context == nil || len(src.Context.Quotes) == 0 {
		return src, groundingUnverified
	}
	reader, canRead := r.reader.(quoteMetadataReader)
	verified := []data.Quote{}
	unknown := []data.Quote{}
	rejected := false
	for _, q := range src.Context.Quotes[:min(len(src.Context.Quotes), maxGroundingQuotes)] {
		if strings.TrimSpace(q.Text) == "" {
			rejected = true
			continue
		}
		props := rec.node(q.ID)
		if props == nil && canRead && q.ID != "" {
			var err error
			props, err = reader.GetNode(ctx, q.ID)
			if err != nil {
				if rec != nil {
					rec.record(errGroundingUnavailable)
				}
				return src, groundingUnreadable
			}
		}
		if v, present := props["grounded"]; present {
			grounded, valid := v.(bool)
			if !valid || !grounded {
				rejected = true
				continue
			}
			verified = append(verified, q)
		} else {
			unknown = append(unknown, q)
		}
	}
	c := *src.Context
	if len(verified) > 0 {
		c.Quotes = verified
		src.Context = &c
		return src, groundingVerified
	}
	if rejected {
		return src, groundingRejected
	}
	c.Quotes = unknown
	src.Context = &c
	return src, groundingUnverified
}
