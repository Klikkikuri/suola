//go:build js && wasm
// +build js,wasm

package main // Build when target is wasm

import (
	"fmt"
	"syscall/js"
)

// hashUrl(url) signs against the base rules; hashUrl(url, name) signs against a named rule set.
//
// A second argument must be a name. A caller that gives undefined, null, a number or an empty string asked
// for a rule set and named none, which the base rules must not answer: the signature would be valid, and no
// later test could find that it came from the wrong rules. Such a call gives null, as an unknown name does.
func hashUrl(this js.Value, args []js.Value) any {
	if len(args) == 0 {
		return nil
	}

	name := ""
	if len(args) > 1 {
		if args[1].Type() != js.TypeString || args[1].String() == "" {
			return nil
		}
		name = args[1].String()
	}

	hash, err := getSignatureIn(name, args[0].String())
	if err != nil {
		return nil
	}
	return hash
}

func appendRulesJS(this js.Value, args []js.Value) any {
	if len(args) == 0 {
		return "rules data required"
	}
	rulesJSON := args[0].String()
	err := AppendRules([]byte(rulesJSON))
	if err != nil {
		return err.Error()
	}
	return nil
}

func loadRulesJS(this js.Value, args []js.Value) any {
	if len(args) == 0 {
		return "rules data required"
	}
	if err := LoadRules([]byte(args[0].String())); err != nil {
		return err.Error()
	}
	return nil
}

func defineRulesJS(this js.Value, args []js.Value) any {
	if len(args) < 2 {
		return "rule set name and rules data required"
	}
	if err := DefineRules(args[0].String(), []byte(args[1].String())); err != nil {
		return err.Error()
	}
	return nil
}

func dropRulesJS(this js.Value, args []js.Value) any {
	if len(args) == 0 {
		return "rule set name required"
	}
	if err := DropRules(args[0].String()); err != nil {
		return err.Error()
	}
	return nil
}

func listRulesJS(this js.Value, args []js.Value) any {
	names := RuleSetNames()
	out := make([]any, len(names))
	for i, name := range names {
		out[i] = name
	}
	return js.ValueOf(out)
}

func RegisterCallbacks() {
	js.Global().Set("hashUrl", js.FuncOf(hashUrl))
	js.Global().Set("appendRules", js.FuncOf(appendRulesJS))
	js.Global().Set("loadRules", js.FuncOf(loadRulesJS))
	js.Global().Set("defineRules", js.FuncOf(defineRulesJS))
	js.Global().Set("dropRules", js.FuncOf(dropRulesJS))
	js.Global().Set("listRules", js.FuncOf(listRulesJS))
}

func main() {
	RegisterCallbacks()
	err := LoadRules(DefaultCfgData)
	if err != nil {
		panic(err)
	}

	fmt.Println("[🧂 suola]: Started.")

	// Prevent Go program from exiting immediately
	select {}
}
