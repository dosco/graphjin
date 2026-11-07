package agent

import axgoja "github.com/ax-llm/ax/packages/go/runtime/goja"

// axFlatFunctionNamespace is the namespace ax gives a function registered
// without one: its prompt inventory lists ask_graphjin as
// utils.ask_graphjin(args: ...). The goja runtime binds a dotted callable name
// into that namespace object, but only when the dotted name is registered.
const axFlatFunctionNamespace = "utils"

// registerAdvertisedCallable registers a host callable under its bare name and
// under the qualified name ax's inventory advertises, for the lead and the
// data agent alike (both inventories list utils.<name>). Registered bare only,
// every call written the way the prompt shows it failed with "ReferenceError:
// utils is not defined": on the lead's context stage the model asked the
// right question three times, failed each time, and handed on a vaguer
// request; other lead runs concluded the data agent was unavailable and
// refused to answer.
func registerAdvertisedCallable[R any](register func(string, axgoja.HostCallable) R, name string, handler axgoja.HostCallable) {
	register(name, handler)
	register(axFlatFunctionNamespace+"."+name, handler)
}
