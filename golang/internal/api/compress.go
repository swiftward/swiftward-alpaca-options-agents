package api

import (
	"compress/gzip"
	"net/http"
	"strings"
	"sync"
)

// Compression, and it is measured rather than assumed.
//
// On 4 September the stand served the page's JavaScript at 649,733 bytes and the
// equity history at 370,034, byte for byte: nothing in front of this process
// compresses, and this process did not either. Both are text, both go down by
// roughly ten times, and on a judge's connection that is the difference between a
// page that appears and a page that is waited for.
//
// It lives here rather than in the proxy's configuration because the same binary
// is what a reader runs from a clone, with nothing in front of it at all. A
// setting in a deployment file would make the copy they run the slow one.

// One writer per response, and they are expensive enough to keep: a gzip.Writer
// carries a 32 KB window it would otherwise allocate on every request.
var gzips = sync.Pool{New: func() any { return gzip.NewWriter(nil) }}

func compressed(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// A range is a request for bytes at an offset OF THE FILE. Compressed, the
		// offsets are of something else, and the answer would be the wrong slice of
		// the right file - so a range is served whole and uncompressed.
		if !strings.Contains(req.Header.Get("Accept-Encoding"), "gzip") || req.Header.Get("Range") != "" {
			next.ServeHTTP(w, req)

			return
		}

		// The answer now depends on a request header, and a cache in between has
		// to be told: without this it can hand a gzipped body to a client that
		// asked for none and cannot read it.
		w.Header().Add("Vary", "Accept-Encoding")

		zipped := &gzipWriter{ResponseWriter: w}
		defer zipped.close()

		next.ServeHTTP(zipped, req)
	})
}

type gzipWriter struct {
	http.ResponseWriter

	zip     *gzip.Writer
	decided bool
}

// WriteHeader is the last moment the headers can still change and the first at
// which the content type is known, so the decision is made here.
func (g *gzipWriter) WriteHeader(status int) {
	g.decide(status)
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	if !g.decided {
		// A handler that writes without WriteHeader means 200. net/http sniffs the
		// type from these same first bytes, and the decision needs it now.
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(p))
		}

		g.decide(http.StatusOK)
	}

	if g.zip != nil {
		return g.zip.Write(p)
	}

	return g.ResponseWriter.Write(p)
}

// Unwrap lets http.ResponseController reach the real writer past this one.
func (g *gzipWriter) Unwrap() http.ResponseWriter {
	return g.ResponseWriter
}

func (g *gzipWriter) Flush() {
	if g.zip != nil {
		_ = g.zip.Flush()
	}

	if flusher, ok := g.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (g *gzipWriter) decide(status int) {
	g.decided = true

	switch {
	// These two carry no body, and a gzip stream of nothing is still eighteen bytes.
	case status == http.StatusNoContent, status == http.StatusNotModified:
		return
	// A handler that encoded its own body owns it.
	case g.Header().Get("Content-Encoding") != "":
		return
	case !compressible(g.Header().Get("Content-Type")):
		return
	}

	g.Header().Set("Content-Encoding", "gzip")
	// The length was counted before compressing and is now wrong. Removed, net/http
	// chunks the answer, which is what it does for every answer of unknown length.
	g.Header().Del("Content-Length")
	// Ranges were of the file, and this is no longer the file.
	g.Header().Del("Accept-Ranges")

	writer, _ := gzips.Get().(*gzip.Writer)
	writer.Reset(g.ResponseWriter)
	g.zip = writer
}

func (g *gzipWriter) close() {
	if g.zip == nil {
		return
	}

	_ = g.zip.Close()
	gzips.Put(g.zip)
	g.zip = nil
}

// compressible answers for a content type. Everything this serves is text - JSON,
// JavaScript, CSS, HTML, an SVG - and what is left out is what arrives compressed
// already: a PNG spends the time and comes out slightly larger.
func compressible(kind string) bool {
	kind = strings.ToLower(strings.TrimSpace(strings.Split(kind, ";")[0]))

	switch {
	case strings.HasPrefix(kind, "text/"):
		return true
	case strings.HasPrefix(kind, "image/svg"):
		return true
	case kind == "application/json",
		kind == "application/javascript",
		kind == "application/manifest+json",
		kind == "application/xml":
		return true
	default:
		return false
	}
}
