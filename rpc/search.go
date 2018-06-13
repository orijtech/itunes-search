package rpc

import (
	"context"
	"log"

	"go.opencensus.io/trace"

	"github.com/orijtech/itunes"
)

type SearchBackend int

var _ SearchServer = (*SearchBackend)(nil)

type query struct {
	err error
	req *Request
}

func (s *SearchBackend) ITunesSearchNonStreaming(ctx context.Context, req *Request) (*Response, error) {
	return searchiTunes(ctx, req)
}

func (s *SearchBackend) ITunesSearchStreaming(srv Search_ITunesSearchStreamingServer) error {
	// Persistent and streaming server

	log.Printf("iTunesSearchStreaming: %v\n", srv)
	queries := make(chan *query, 100)
	// Receive routine asynchronously running to ensure that we
	// can accumulate very many requests without having to wait for a response
	go func() {
		defer close(queries)

		for {
			req, err := srv.Recv()
			if err != nil {
				queries <- &query{err: err}
				return
			}
			queries <- &query{req: req}
		}
	}()

	// Response routine
	for query := range queries {
		if query.err != nil {
			return query.err
		}

		req := query.req
		// Otherwise now perform the search
		ctx, span := trace.StartSpan(context.Background(), "(*search).ITunesSearch")
		resp, err := searchiTunes(ctx, req)
		span.End()
		if err != nil {
			if resp == nil {
				resp = &Response{Err: err.Error()}
			}
		}
		resp.RequestId = req.Id
		if err := srv.Send(resp); err != nil {
			return err
		}
	}
	return nil
}

func searchiTunes(ctx context.Context, req *Request) (*Response, error) {
	ctx, span := trace.StartSpan(ctx, "searchiTunes")
	defer span.End()

	ic := new(itunes.Client)
	sres, err := ic.Search(ctx, &itunes.Search{
		Term:            req.Query,
		Country:         itunes.Country(req.Country),
		ExplicitContent: req.Explicit,
		Language:        itunes.Language(req.Language),
	})
	if err != nil {
		return nil, err
	}
	results := make([]*Result, 0, len(sres.Results))
	for _, rr := range sres.Results {
		results = append(results, iTunesResultToResponse(rr))
	}
	res := &Response{
		ResultCount: sres.ResultCount,
		Results:     results,
	}
	return res, nil
}

func iTunesResultToResponse(sres *itunes.Result) *Result {
	return &Result{
		PreviewUrl:      sres.PreviewURL,
		Streamable:      sres.Streamable,
		ArtistName:      sres.ArtistName,
		Kind:            sres.Kind,
		Country:         sres.Country,
		TrackName:       sres.TrackName,
		TrackPrice:      sres.TrackPrice,
		CollectionPrice: sres.CollectionPrice,
		Currency:        sres.Currency,
		TrackNumber:     int32(sres.TrackNumber),
		ArtworkUrl:      sres.ArtworkURL100Px,
		ArtistUrl:       sres.ArtistViewURL,
		TrackViewUrl:    sres.TrackViewURL,
	}
}
