# JSN - Fast low allocation JSON library
## Design

This libary is designed as a set of seperate functions to extract data and mutate
JSON. All functions are focused on keeping allocations to a minimum and be as fast
as possible. The functions don't validate the JSON a seperate `Validate` function
does that. 

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
