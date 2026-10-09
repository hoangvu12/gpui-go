package accessspec

// Port-recorded golden fixtures for the accessibility corpus traces
// (ticket21). HONEST SOURCE NOTE: these are traces produced by THIS
// port's deterministic tree builder, recorded once and pinned here;
// the repository's reference-fixture harness (ticket01) recorded no
// accessibility trace fixtures, so there is no CE-recorded a11y trace
// to compare against yet. The determinism guarantee that makes the
// golden files meaningful is the node-id policy: ids derive from the
// canonical element id paths only (no addresses, map order or run
// order), so the traces are byte-identical across runs and machines.
//
// Regenerating: temporarily replace the constants' values with the
// Trace() output of the corresponding test snapshots (the tests print
// the full trace on mismatch).
const (
	// fixtureFullTreeTrace is the trace of the first complete publish
	// after activation (sequence 2): root, container, label, button,
	// editor and its three synthetic text runs, nothing focused.
	fixtureFullTreeTrace = `window "Counter App" gen=1 seq=2 nodes=8 focus=#0000000000000000
#0000000000000000 Window label="Counter App" bounds=(0, 0, 0 x 0)
  #68ece2ea00328d43 GenericContainer id="counter-app" bounds=(0, 0, 264 x 124)
    #934ff1a497816d14 StaticText id="count-label" value="Count: 0" bounds=(12, 12, 160 x 24)
    #d65374e5d46abb95 Button id="counter.increment" label="Increment" focus-handle=1 actions=[Click,Increment] bounds=(12, 44, 96 x 32)
    #ddad0d53b1cb9281 TextInput id="editor" label="Editor" description="caret at byte 0" value="hello a11y world" focus-handle=2 actions=[Focus,SetValue,ReplaceSelectedText] bounds=(12, 84, 240 x 28)
      #c1fcfa42f106bac5 TextRun value="hello" synthetic bounds=(12, 84, 240 x 28)
      #c1fcf942f106b912 TextRun value="a11y" synthetic bounds=(12, 84, 240 x 28)
      #c1fcf842f106b75f TextRun value="world" synthetic bounds=(12, 84, 240 x 28)
`

	// fixtureUpdatedTreeTrace is the trace after two Clicks and one
	// SetValue: the same node ids with updated values, the button
	// focused (the Click handler focuses it through the real focus
	// machinery), the editor text replaced by the action data.
	fixtureUpdatedTreeTrace = `window "Counter App" gen=1 seq=3 nodes=8 focus=#d65374e5d46abb95
#0000000000000000 Window label="Counter App" bounds=(0, 0, 0 x 0)
  #68ece2ea00328d43 GenericContainer id="counter-app" bounds=(0, 0, 264 x 124)
    #934ff1a497816d14 StaticText id="count-label" value="Count: 2" bounds=(12, 12, 160 x 24)
    #d65374e5d46abb95 Button id="counter.increment" label="Increment" focus-handle=1 actions=[Click,Increment] bounds=(12, 44, 96 x 32)
    #ddad0d53b1cb9281 TextInput id="editor" label="Editor" description="caret at byte 0" value="typed by a11y" focus-handle=2 actions=[Focus,SetValue,ReplaceSelectedText] bounds=(12, 84, 240 x 28)
      #c1fcfa42f106bac5 TextRun value="typed" synthetic bounds=(12, 84, 240 x 28)
      #c1fcf942f106b912 TextRun value="by" synthetic bounds=(12, 84, 240 x 28)
      #c1fcf842f106b75f TextRun value="a11y" synthetic bounds=(12, 84, 240 x 28)
`
)
