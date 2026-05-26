# JSN - Fast low allocation JSON library
## Design

This libary is designed as a set of seperate functions to extract data and mutate
JSON. All functions are focused on keeping allocations to a minimum and be as fast
as possible. The functions don't validate the JSON a seperate `Validate` function
does that. 

## Go JSON v2

Go 1.25 introduced experimental `encoding/json/v2` and `encoding/json/jsontext`
packages behind `GOEXPERIMENT=jsonv2`. They are worth tracking, especially for
strict JSON decoding workloads, but they are not a drop-in replacement for this
package today. The hot GraphJin uses here need tolerant, raw-slice helpers for
remote joins, filtering, stripping, and replacement without unmarshaling into Go
values. Keep this package on the custom scanner path unless a benchmark on those
workloads shows a clear win.

## Validation guardrails

`Validate` is the strict path for untrusted JSON. It rejects invalid UTF-8 in
object keys and string values, and caps nesting depth to prevent recursive
parser exhaustion. The scanner helpers remain tolerant and best-effort because
GraphJin uses them on generated and already-validated response fragments.

The JSON parsing algo processes each object `{}` or array `[]` in a bottom up way
where once the end of the array or object is found only then the keys within it are 
parsed from the top down.

```
{"id":1,"posts": [{"title":"PT1-1","description":"PD1-1"}], "full_name":"FN1","email":"E1" }

id: 1

posts: [{"title":"PT1-1","description":"PD1-1"}]

[{"title":"PT1-1","description":"PD1-1"}]

{"title":"PT1-1","description":"PD1-1"}

title: "PT1-1"

description: "PD1-1

full_name: "FN1"

email: "E1"
```

## Functions

- Strip: Strip a path from the root to a child node and return the rest
- Replace: Replace values by key
- Get: Get all keys
- Filter: Extract specific keys from an object
- Tree: Fetch unique keys from an array or object
