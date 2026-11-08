package ytm

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"ytb-music/internal/jsc"
)

// The web clients' streams come with their URLs scrambled, as YouTube's
// player script unscrambles them: the signature ("s", put back as "sig"),
// and the "n" that a stream serves slowly without. yt-dlp's solver
// (github.com/yt-dlp/ejs 0.8.0, the Unlicense, with meriyah, ISC, and
// astring, MIT) finds the player's function for both, and runs in
// JavaScriptCore. When YouTube changes its player past what the solver
// finds, a newer release of these two files does.
var (
	//go:embed ejs/yt.solver.lib.min.js
	ejsLib string
	//go:embed ejs/yt.solver.core.js
	ejsCore string
)

// PlayerCacheDir is where the players worked out are kept between runs,
// which takes a couple of seconds a player; the app sets it.
var PlayerCacheDir string

// solver unscrambles the streams of one version of the player.
type solver struct {
	id  string // the player's version
	sts int    // its signatureTimestamp, which streams are scrambled for
	ctx *jsc.Context
}

var solvers struct {
	mu      sync.Mutex
	cur     *solver
	checked time.Time // when the player's version was last asked
}

// playerCheck is how often the player's version is asked: it changes
// every few days.
const playerCheck = 6 * time.Hour

// PreparePlayer works out the player YouTube serves now, if it hasn't
// yet, in the background: the first song at "exhigh" would wait seconds
// for it.
func PreparePlayer() {
	go currentSolver()
}

// currentSolver returns the solver of the player YouTube serves now.
func currentSolver() (*solver, error) {
	solvers.mu.Lock()
	defer solvers.mu.Unlock()
	if solvers.cur != nil && time.Since(solvers.checked) < playerCheck {
		return solvers.cur, nil
	}
	id, err := playerVersion()
	if err != nil {
		if solvers.cur != nil {
			return solvers.cur, nil
		}
		return nil, err
	}
	solvers.checked = time.Now()
	if solvers.cur != nil && solvers.cur.id == id {
		return solvers.cur, nil
	}
	s, err := newSolver(id)
	if err != nil {
		return nil, err
	}
	if solvers.cur != nil {
		solvers.cur.ctx.Close()
	}
	solvers.cur = s
	return s, nil
}

var playerRe = regexp.MustCompile(`\\?/s\\?/player\\?/([0-9a-zA-Z_-]{8,})\\?/`)

// playerVersion asks which player YouTube's embeds load, from the few
// lines of its iframe API.
func playerVersion() (string, error) {
	body, _, err := get("https://www.youtube.com/iframe_api", UserAgent)
	if err != nil {
		return "", err
	}
	m := playerRe.FindSubmatch(body)
	if m == nil {
		return "", errors.New("ytm: no player version")
	}
	return string(m[1]), nil
}

var stsRe = regexp.MustCompile(`(?:signatureTimestamp|sts):(\d{5})`)

// newSolver loads the player id, worked out once, from the cache, else
// works it out from its script.
func newSolver(id string) (*solver, error) {
	path := ""
	if PlayerCacheDir != "" {
		path = filepath.Join(PlayerCacheDir, id+".js")
	}
	var prepared string
	sts := 0
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			head, rest, _ := strings.Cut(string(b), "\n")
			if n, err := strconv.Atoi(strings.TrimPrefix(head, "// sts:")); err == nil {
				prepared, sts = rest, n
			}
		}
	}
	if prepared == "" {
		js, _, err := get("https://www.youtube.com/s/player/"+id+"/player_ias.vflset/en_US/base.js", UserAgent)
		if err != nil {
			return nil, err
		}
		m := stsRe.FindSubmatch(js)
		if m == nil {
			return nil, errors.New("ytm: the player has no signatureTimestamp")
		}
		sts, _ = strconv.Atoi(string(m[1]))
		if prepared, err = preparePlayer(string(js)); err != nil {
			return nil, err
		}
		if path != "" && os.MkdirAll(PlayerCacheDir, 0o755) == nil {
			os.WriteFile(path, []byte(fmt.Sprintf("// sts:%d\n%s", sts, prepared)), 0o644)
		}
	}
	ctx, err := jsc.New()
	if err != nil {
		return nil, err
	}
	// As the solver's getFromPrepared runs it: the player's code, which
	// leaves its functions in _result.
	ok, err := ctx.Eval("var _result = {n: null, sig: null};\n(function (_result) {\n" + prepared + "\n})(_result);\n!!(_result.n && _result.sig)")
	if err != nil || ok != "true" {
		ctx.Close()
		if err == nil {
			err = errors.New("ytm: the solver found no functions in the player")
		}
		return nil, err
	}
	return &solver{id: id, sts: sts, ctx: ctx}, nil
}

// preparePlayer has the solver find the player's functions and return the
// player's code cut down to them.
func preparePlayer(js string) (string, error) {
	player, _ := json.Marshal(js)
	out, err := jsc.Eval(ejsLib + "\nvar meriyah = lib.meriyah, astring = lib.astring;\n" + ejsCore +
		"\nvar _out = jsc({type: 'player', player: " + string(player) + ", requests: [], output_preprocessed: true});\n_out.preprocessed_player")
	if err != nil {
		return "", err
	}
	if out == "" || out == "undefined" {
		return "", errors.New("ytm: the solver prepared nothing")
	}
	return out, nil
}

// solve unscrambles a stream's signature, if it has one, and its n.
func (s *solver) solve(sig, n string) (string, string, error) {
	js := func(v string) string { b, _ := json.Marshal(v); return string(b) }
	script := "JSON.stringify({"
	if sig != "" {
		script += "sig: _result.sig(" + js(sig) + "), "
	}
	if n != "" {
		script += "n: _result.n(" + js(n) + ")"
	}
	out, err := s.ctx.Eval(script + "})")
	if err != nil {
		return "", "", err
	}
	var r struct{ Sig, N string }
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return "", "", err
	}
	if sig != "" && r.Sig == "" || n != "" && r.N == "" {
		return "", "", errors.New("ytm: the player's functions gave nothing")
	}
	return r.Sig, r.N, nil
}

// streamURL returns where a format of a web client's streams, its url or
// its signatureCipher, serves, unscrambled by solve.
func streamURL(format any, solve func(sig, n string) (string, string, error)) (string, error) {
	raw, sig, sp := str(format, "url"), "", ""
	if raw == "" {
		q, err := url.ParseQuery(str(format, "signatureCipher"))
		if err != nil {
			return "", err
		}
		raw, sig, sp = q.Get("url"), q.Get("s"), q.Get("sp")
		if sp == "" {
			sp = "signature"
		}
	}
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return "", errors.New("ytm: the format has no URL")
	}
	q := u.Query()
	n := q.Get("n")
	if sig == "" && n == "" {
		return raw, nil
	}
	sig, n, err = solve(sig, n)
	if err != nil {
		return "", err
	}
	if sp != "" {
		q.Set(sp, sig)
	}
	if n != "" {
		q.Set("n", n)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}
