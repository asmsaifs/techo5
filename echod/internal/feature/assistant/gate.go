package assistant

import "github.com/HuskerMinion/techo5/echod/internal/lib/llm"

// TurnGate is one turn's lockout after reading outside words (webTurn), for whatever runs tools in a turn:
// this package's own loop, and realtime voice (feature/voice), whose model calls the device's tools and
// Home Assistant's. One lockout for both, so a turn that has read the web or a calendar changes nothing,
// whichever model it is.
//
// A turn goes in rounds: the calls a model asks for together are one round, and what a round read holds
// from the next round on (EndRound). A model that asks for one call at a time makes every call a round.
type TurnGate struct {
	ts   []tool
	web  webTurn
	read bool // the turn has read outside words
}

// NewTurnGate is a new turn's.
func NewTurnGate() *TurnGate { return &TurnGate{ts: toolList()} }

// Tools is the device's own tools a turn may use.
func (g *TurnGate) Tools() []llm.Tool { return specs(g.ts) }

// Run runs one of the device's own tools under the lockout, and answers what happened in words the
// model reads.
func (g *TurnGate) Run(call llm.ToolCall) string {
	result := g.web.check(g.ts, call)
	g.read = g.read || readsTheWeb[call.Function.Name]
	return result
}

// Allow is the lockout for a tool that is not the device's own (Home Assistant's): readOnly is one that
// only looks something up. Anything else is refused in a turn that has read outside words, with what the
// model is told.
func (g *TurnGate) Allow(readOnly bool) (ok bool, refused string) {
	if g.web.from && !readOnly {
		return false, refusedAfterWeb
	}
	return true, ""
}

// Saw notes a tool that is not the device's own having brought outside words into the turn: a calendar's
// events, a search's results.
func (g *TurnGate) Saw(outside bool) {
	if outside {
		g.read = true
		g.web.moreNow = true
	}
}

// EndRound is a round of calls done: what it read holds from the next round on.
func (g *TurnGate) EndRound() { g.web.endRound(g.read) }

// Read is whether the turn has read outside words, which decides what of it is kept for a follow-up.
func (g *TurnGate) Read() bool { return g.read }
